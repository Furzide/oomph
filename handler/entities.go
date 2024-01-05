package handler

import (
	"github.com/go-gl/mathgl/mgl32"
	"github.com/oomph-ac/oomph/entity"
	"github.com/oomph-ac/oomph/player"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const HandlerIDEntities = "oomph:entities"
const DefaultEntityHistorySize = 6

// EntityHandler handles entities and their respective positions to the client. On AuthorityModeSemi, EntityHandler is able to
// replicate a 1:1 position of what the client sees, which is used for detections. On AuthorityModeComplete, EntityHandler uses rewind
// to determine entity positions based on the client tick, and is used for full authority over combat.
type EntityHandler struct {
	Entities       map[uint64]*entity.Entity
	MaxRewindTicks int
}

func NewEntityHandler() *EntityHandler {
	return &EntityHandler{
		Entities:       make(map[uint64]*entity.Entity),
		MaxRewindTicks: DefaultEntityHistorySize,
	}
}

func (h *EntityHandler) ID() string {
	return HandlerIDEntities
}

func (h *EntityHandler) HandleClientPacket(pk packet.Packet, p *player.Player) bool {
	if _, ok := pk.(*packet.PlayerAuthInput); ok && p.CombatMode == player.AuthorityModeSemi {
		h.tickEntities(p.ServerTick)
	}

	return true
}

func (h *EntityHandler) HandleServerPacket(pk packet.Packet, p *player.Player) bool {
	switch pk := pk.(type) {
	case *packet.AddActor:
		h.AddEntity(pk.EntityRuntimeID, entity.New(pk.Position, pk.Velocity, h.MaxRewindTicks, false))
	case *packet.AddPlayer:
		h.AddEntity(pk.EntityRuntimeID, entity.New(pk.Position, pk.Velocity, h.MaxRewindTicks, true))
	case *packet.RemoveActor:
		h.RemoveEntity(uint64(pk.EntityUniqueID))
	case *packet.MoveActorAbsolute:
		if pk.EntityRuntimeID == p.RuntimeId {
			return true
		}

		// If the authority mode is set to AuthorityModeSemi, we need to wait for the client to acknowledge the
		// position before the entity is moved.
		if p.CombatMode == player.AuthorityModeSemi {
			p.Handler(HandlerIDAcknowledgements).(*AcknowledgementHandler).AddCallback(func() {
				h.moveEntity(pk.EntityRuntimeID, p.ServerTick, pk.Position)
			})
			return true
		}

		h.moveEntity(pk.EntityRuntimeID, p.ServerTick, pk.Position)
	case *packet.MovePlayer:
		if pk.EntityRuntimeID == p.RuntimeId {
			return true
		}

		// If the authority mode is set to AuthorityModeSemi, we need to wait for the client to acknowledge the
		// position before the entity is moved.
		if p.CombatMode == player.AuthorityModeSemi {
			p.Handler(HandlerIDAcknowledgements).(*AcknowledgementHandler).AddCallback(func() {
				h.moveEntity(pk.EntityRuntimeID, p.ServerTick, pk.Position)
			})
			return true
		}

		h.moveEntity(pk.EntityRuntimeID, p.ServerTick, pk.Position)
	case *packet.SetActorMotion:
		if pk.EntityRuntimeID == p.RuntimeId {
			return true
		}

		entity := h.FindEntity(pk.EntityRuntimeID)
		if entity == nil {
			return true
		}

		p.Handler(HandlerIDAcknowledgements).(*AcknowledgementHandler).AddCallback(func() {
			entity.RecvVelocity = pk.Velocity
		})
	case *packet.SetActorData:
		width, widthExists := pk.EntityMetadata[entity.DataKeyBoundingBoxWidth]
		height, heightExists := pk.EntityMetadata[entity.DataKeyBoundingBoxHeight]

		e := h.FindEntity(pk.EntityRuntimeID)
		if e == nil {
			return true
		}

		if widthExists {
			e.Width = width.(float32)
		}

		if heightExists {
			e.Height = height.(float32)
		}
	}

	return true
}

func (h *EntityHandler) OnTick(p *player.Player) {
	if p.CombatMode != player.AuthorityModeComplete {
		return
	}

	h.tickEntities(p.ServerTick)
}

// AddEntity adds an entity to the entity handler.
func (h *EntityHandler) AddEntity(rid uint64, e *entity.Entity) {
	h.Entities[rid] = e
}

// FindEntity returns an entity from the given runtime ID. Nil is returned if the entity does not exist.
func (h *EntityHandler) FindEntity(rid uint64) *entity.Entity {
	return h.Entities[rid]
}

// RemoveEntity removes an entity from the entity handler.
func (h *EntityHandler) RemoveEntity(rid uint64) {
	delete(h.Entities, rid)
}

// moveEntity moves an entity to the given position.
func (h *EntityHandler) moveEntity(rid uint64, tick int64, pos mgl32.Vec3) {
	e := h.FindEntity(rid)
	if e == nil {
		return
	}

	if e.IsPlayer {
		pos[1] -= 1.62
	}

	e.RecievePosition(entity.HistoricalPosition{
		Position: pos,
		Tick:     tick,
	})
}

// tickEntities ticks all the entities in the entity handler.
func (h *EntityHandler) tickEntities(tick int64) {
	for _, e := range h.Entities {
		e.Tick(tick)
	}
}
