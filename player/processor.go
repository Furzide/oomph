package player

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/oomph-ac/oomph/entity"
	"github.com/oomph-ac/oomph/game"
	"github.com/oomph-ac/oomph/utils"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"math"
	"time"
	_ "unsafe"
)

// ClientProcess processes the given packet from the client.
func (p *Player) ClientProcess(pk packet.Packet) bool {
	p.clicking = false

	switch pk := pk.(type) {
	case *packet.NetworkStackLatency:
		p.ackMu.Lock()
		call, ok := p.acknowledgements[pk.Timestamp]
		if ok {
			delete(p.acknowledgements, pk.Timestamp)
			p.ackMu.Unlock()

			call()
			return true
		}
		p.ackMu.Unlock()
		return false
	case *packet.PlayerAuthInput:
		p.clientTick++
		p.Move(pk)

		if utils.HasFlag(pk.InputData, packet.InputFlagStartSprinting) || utils.HasFlag(pk.InputData, packet.InputFlagStopSprinting) {
			p.sprinting = !p.sprinting
		} else if utils.HasFlag(pk.InputData, packet.InputFlagStartSneaking) || utils.HasFlag(pk.InputData, packet.InputFlagStopSneaking) {
			p.sneaking = !p.sneaking
		}
		p.jumping = utils.HasFlag(pk.InputData, packet.InputFlagStartJumping)

		pos := p.Position()
		p.inVoid = pos.Y() <= game.VoidLevel

		p.jumpVelocity = game.DefaultJumpMotion
		p.speed = game.NormalMovementSpeed
		p.gravity = game.NormalGravity

		p.tickEffects()

		p.moveStrafe = float64(pk.MoveVector.X() * 0.98)
		p.moveForward = float64(pk.MoveVector.Y() * 0.98)

		if p.Sprinting() {
			p.speed *= 1.3
		}
		p.speed = math.Max(0, p.speed)

		p.tickMovement()
		p.tickNearbyBlocks()
		p.tickEntityLocations()
		p.teleporting = false
	case *packet.LevelSoundEvent:
		if pk.SoundType == packet.SoundEventAttackNoDamage {
			p.Click()
		}
		return false
	case *packet.InventoryTransaction:
		if _, ok := pk.TransactionData.(*protocol.UseItemOnEntityTransactionData); ok {
			p.Click()
		} else if t, ok := pk.TransactionData.(*protocol.UseItemTransactionData); ok && t.ActionType == protocol.UseItemActionClickBlock {
			pos := cube.Pos{int(t.BlockPosition.X()), int(t.BlockPosition.Y()), int(t.BlockPosition.Z())}
			block, ok := world.BlockByRuntimeID(t.BlockRuntimeID)
			if !ok {
				// Block somehow doesn't exist, so do nothing.
				return false
			}

			w := p.World()
			boxes := block.Model().AABB(pos, w)
			for _, box := range boxes {
				if box.Translate(pos.Vec3()).IntersectsWith(p.AABB().Translate(p.Position())) {
					// Intersects with our AABB, so do nothing.
					return false
				}
			}

			// Tada.
			w.SetBlock(pos, block, nil)
		}
	case *packet.ChunkRadiusUpdated:
		p.Loader().ChangeRadius(int(pk.ChunkRadius))
		return false
	case *packet.AdventureSettings:
		p.flying = utils.HasFlag(uint64(pk.Flags), packet.AdventureFlagFlying)
		return false
	case *packet.Respawn:
		if pk.EntityRuntimeID == p.rid && pk.State == packet.RespawnStateClientReadyToSpawn {
			p.dead = false
		}
		return false
	case *packet.Text:
		if p.serverConn != nil {
			// Strip the XUID to prevent certain server software from flagging the message as spam.
			pk.XUID = ""
		}
		return false
	}

	// Run all registered checks.
	p.checkMu.Lock()
	defer p.checkMu.Unlock()
	for _, c := range p.checks {
		c.Process(p, pk)
	}
	return false
}

// ServerProcess processes the given packet from the server.
func (p *Player) ServerProcess(pk packet.Packet) bool {
	switch pk := pk.(type) {
	case *packet.AddPlayer:
		if pk.EntityRuntimeID == p.rid {
			// We are the player.
			return false
		}

		p.Acknowledgement(func() {
			p.AddEntity(pk.EntityRuntimeID, entity.NewEntity(
				game.Vec32To64(pk.Position),
				game.Vec32To64(pk.Velocity),
				game.Vec32To64(mgl32.Vec3{pk.Pitch, pk.HeadYaw, pk.Yaw}),
				true,
			))
		})
	case *packet.AddActor:
		if pk.EntityRuntimeID == p.rid {
			// We are the player.
			return false
		}

		p.Acknowledgement(func() {
			p.AddEntity(pk.EntityRuntimeID, entity.NewEntity(
				game.Vec32To64(pk.Position),
				game.Vec32To64(pk.Velocity),
				game.Vec32To64(mgl32.Vec3{pk.Pitch, pk.HeadYaw, pk.Yaw}),
				false,
			))
		})
	case *packet.MoveActorAbsolute:
		if pk.EntityRuntimeID == p.rid {
			p.Acknowledgement(func() {
				p.Teleport(pk.Position)
				if utils.HasFlag(uint64(pk.Flags), packet.MoveFlagTeleport) {
					p.teleporting = true
				}
			})
			return false
		}

		p.MoveEntity(pk.EntityRuntimeID, game.Vec32To64(pk.Position))
	case *packet.MovePlayer:
		if pk.EntityRuntimeID == p.rid {
			p.Acknowledgement(func() {
				p.Teleport(pk.Position)
				if pk.Mode == packet.MoveModeTeleport {
					p.teleporting = true
				}
			})
			return false
		}

		p.MoveEntity(pk.EntityRuntimeID, game.Vec32To64(pk.Position))
	case *packet.LevelChunk:
		p.Acknowledgement(func() {
			a, _ := chunk.StateToRuntimeID("minecraft:air", nil)
			c, err := chunk.NetworkDecode(a, pk.RawPayload, int(pk.SubChunkCount), p.w.Range())
			if err != nil {
				p.log.Errorf("failed to parse chunk at %v: %v", pk.Position, err)
				return
			}

			world_setChunk(p.w, world.ChunkPos(pk.Position), c, nil)
			p.ready = true
		})
	case *packet.UpdateBlock:
		b, ok := world.BlockByRuntimeID(pk.NewBlockRuntimeID)
		if ok {
			p.Acknowledgement(func() {
				p.World().SetBlock(cube.Pos{int(pk.Position.X()), int(pk.Position.Y()), int(pk.Position.Z())}, b, nil)
			})
		}
	case *packet.SetActorData:
		p.Acknowledgement(func() {
			width, widthExists := pk.EntityMetadata[entity.DataKeyBoundingBoxWidth]
			height, heightExists := pk.EntityMetadata[entity.DataKeyBoundingBoxHeight]
			if e, ok := p.SearchEntity(pk.EntityRuntimeID); ok && widthExists && heightExists {
				e.SetAABB(game.AABBFromDimensions(float64(width.(float32)), float64(height.(float32))))
			}

			if f, ok := pk.EntityMetadata[entity.DataKeyFlags]; pk.EntityRuntimeID == p.rid && ok {
				p.immobile = utils.HasDataFlag(entity.DataFlagImmobile, f.(int64))
			}
		})
	case *packet.SetActorMotion:
		if pk.EntityRuntimeID == p.rid {
			p.Acknowledgement(func() {
				p.motionTicks = 0
				p.serverSentMotion = pk.Velocity
			})
		}
	case *packet.SetPlayerGameType:
		p.Acknowledgement(func() {
			p.gameMode = pk.GameType
		})
	case *packet.RemoveActor:
		if pk.EntityUniqueID != p.uid {
			p.Acknowledgement(func() {
				p.RemoveEntity(uint64(pk.EntityUniqueID))
			})
		}
	case *packet.UpdateAttributes:
		if pk.EntityRuntimeID == p.rid {
			p.Acknowledgement(func() {
				for _, a := range pk.Attributes {
					if a.Name == "minecraft:health" && a.Value <= 0 {
						p.dead = true
					}
				}
			})
		}
	case *packet.MobEffect:
		if pk.EntityRuntimeID == p.rid {
			p.Acknowledgement(func() {
				switch pk.Operation {
				case packet.MobEffectAdd, packet.MobEffectModify:
					if t, ok := effect.ByID(int(pk.EffectType)); ok {
						if t, ok := t.(effect.LastingType); ok {
							eff := effect.New(t, int(pk.Amplifier+1), time.Duration(pk.Duration*50)*time.Millisecond)
							p.SetEffect(pk.EffectType, eff)
						}
					}
				case packet.MobEffectRemove:
					p.RemoveEffect(pk.EffectType)
				}
			})
		}
	}
	return false
}

//go:linkname world_setChunk github.com/df-mc/dragonfly/server/world.(*World).setChunk
//noinspection ALL
func world_setChunk(w *world.World, pos world.ChunkPos, c *chunk.Chunk, e map[cube.Pos]world.Block)
