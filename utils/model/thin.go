package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/entity/physics"
	"github.com/go-gl/mathgl/mgl64"
)

// Thin is a model for thin, partial blocks such as a glass pane or an iron bar. It changes its bounding box depending
// on solid faces next to it.
type Thin struct{ model.Thin }

// AABB returns a slice of physics.AABB that depends on the blocks surrounding the Thin block. Thin blocks can connect
// to any other Thin block, wall or solid faces of other blocks.
func (t Thin) AABB(pos cube.Pos, w HasWorld) []physics.AABB {
	const offset = 0.4375

	boxes := make([]physics.AABB, 0, 5)
	mainBox := physics.NewAABB(mgl64.Vec3{offset, 0, offset}, mgl64.Vec3{1 - offset, 1, 1 - offset})

	for _, f := range cube.HorizontalFaces() {
		pos := pos.Side(f)
		block := w.Block(pos)

		if _, thin := block.Model().(model.Thin); thin || block.Model().FaceSolid(pos, f.Opposite(), nil) {
			boxes = append(boxes, mainBox.ExtendTowards(f, offset))
		}
	}
	return append(boxes, mainBox)
}

// FaceSolid returns true if the face passed is cube.FaceDown.
func (t Thin) FaceSolid(_ cube.Pos, face cube.Face, _ HasWorld) bool {
	return face == cube.FaceDown
}
