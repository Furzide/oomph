package player

import (
	"context"
	"net"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	GameVersion1_20_0  = 589
	GameVersion1_20_10 = 594
	GameVersion1_20_30 = 618
	GameVersion1_20_40 = 622
)

// IdentityData returns the login.IdentityData of a player. It contains the UUID, XUID and username of the connection.
func (p *Player) IdentityData() login.IdentityData {
	return p.conn.IdentityData()
}

// ClientData returns the login.ClientData of a player. This includes less sensitive data of the player like its skin,
// language code and other non-essential information.
func (p *Player) ClientData() login.ClientData {
	return p.conn.ClientData()
}

// ClientCacheEnabled specifies if the conn has the client cache, used for caching chunks client-side, enabled or
// not. Some platforms, like the Nintendo Switch, have this disabled at all times.
func (p *Player) ClientCacheEnabled() bool {
	// todo: support client cache
	//return p.conn.ClientCacheEnabled()
	return false
}

// ChunkRadius returns the chunk radius as requested by the client at the other end of the conn.
func (p *Player) ChunkRadius() int {
	return p.conn.ChunkRadius()
}

// Latency returns the current latency measured over the conn.
func (p *Player) Latency() time.Duration {
	return p.conn.Latency()
}

// Flush flushes the packets buffered by the conn, sending all of them out immediately.
func (p *Player) Flush() error {
	return p.conn.Flush()
}

// RemoteAddr returns the remote network address.
func (p *Player) RemoteAddr() net.Addr {
	return p.conn.RemoteAddr()
}

// WritePacket will call minecraft.Conn.WritePacket and process the packet with oomph.
func (p *Player) WritePacket(pk packet.Packet) error {
	p.StartHandlePacket()
	if p.ServerProcess(pk) {
		p.EndHandlePacket()
		return nil
	}

	if err := p.conn.WritePacket(pk); err != nil {
		p.EndHandlePacket()
		p.Close()
		return err
	}

	p.EndHandlePacket()
	return nil
}

// ReadPacket will call minecraft.Conn.ReadPacket and process the packet with oomph.
func (p *Player) ReadPacket() (pk packet.Packet, err error) {
	p.StartHandlePacket()

	p.tMu.Lock()
	if len(p.toSend) > 0 {
		pk = p.toSend[0]
		p.toSend = p.toSend[1:]
		p.tMu.Unlock()
		p.EndHandlePacket()

		return pk, err
	}
	p.tMu.Unlock()

	if pk, err = p.conn.ReadPacket(); err != nil {
		p.EndHandlePacket()
		p.Close()
		return nil, err
	}

	/* if p.usePacketBuffer {
		if p.QueuePacket(pk) {
			return pk, err
		}

		return p.ReadPacket()
	} */

	if p.ClientProcess(pk) {
		p.EndHandlePacket()
		return p.ReadPacket()
	}

	p.EndHandlePacket()
	return pk, err
}

// StartGameContext starts the game for the conn with a context to cancel it.
func (p *Player) StartGameContext(ctx context.Context, data minecraft.GameData) error {
	data.PlayerMovementSettings.MovementType = protocol.PlayerMovementModeServerWithRewind
	data.PlayerMovementSettings.RewindHistorySize = 100

	p.mInfo.ServerPosition = data.PlayerPosition.Sub(mgl32.Vec3{0, 1.62})
	p.mInfo.OnGround = true

	return p.conn.StartGameContext(ctx, data)
}
