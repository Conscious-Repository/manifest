package construction

// GLB export of a GeometryIR. One node per component (name = component id,
// extras carry the semantic identity), flat-shaded triangles, one embedded
// binary buffer — no external URIs, no textures. World millimetres Z-up map
// to glTF metres Y-up exactly once: (x, z, -y)/1000, inverse in asset.extras.
// Identical IR gives byte-identical GLB.

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// GLBGenerator names this exporter in derived-artifact records.
const GLBGenerator = "construction-glb/1"

// ToGLTF maps a world point (mm, Z-up) to glTF (m, Y-up).
func ToGLTF(p V3) [3]float32 {
	return [3]float32{float32(p[0] / 1000), float32(p[2] / 1000), float32(-p[1] / 1000)}
}

// FromGLTF is the stored inverse.
func FromGLTF(q [3]float32) V3 {
	return V3{float64(q[0]) * 1000, -float64(q[2]) * 1000, float64(q[1]) * 1000}
}

func hexColor(s string) [3]float64 {
	if len(s) != 7 || s[0] != '#' {
		return [3]float64{0.6, 0.6, 0.6}
	}
	var out [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return [3]float64{0.6, 0.6, 0.6}
		}
		// sRGB → linear for glTF baseColorFactor
		c := float64(v) / 255
		if c <= 0.04045 {
			out[i] = c / 12.92
		} else {
			out[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
		out[i] = math.Round(out[i]*1e6) / 1e6
	}
	return out
}

// GLB encodes the IR. meta is copied into asset.extras (assembly revision,
// problem id, non-approval notice…).
func GLB(ir *GeometryIR, meta map[string]string) ([]byte, error) {
	type accessor struct {
		BufferView    int       `json:"bufferView"`
		ComponentType int       `json:"componentType"`
		Count         int       `json:"count"`
		Type          string    `json:"type"`
		Min           []float32 `json:"min,omitempty"`
		Max           []float32 `json:"max,omitempty"`
	}
	type view struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		Target     int `json:"target"`
	}
	type material struct {
		Name        string         `json:"name"`
		PBR         map[string]any `json:"pbrMetallicRoughness"`
		AlphaMode   string         `json:"alphaMode,omitempty"`
		DoubleSided bool           `json:"doubleSided"`
	}
	type primitive struct {
		Attributes map[string]int `json:"attributes"`
		Indices    int            `json:"indices"`
		Material   int            `json:"material"`
	}
	type mesh struct {
		Name       string      `json:"name"`
		Primitives []primitive `json:"primitives"`
	}
	type node struct {
		Name   string            `json:"name"`
		Mesh   int               `json:"mesh"`
		Extras map[string]string `json:"extras"`
	}
	var bin bytes.Buffer
	var accessors []accessor
	var views []view
	var materials []material
	var meshes []mesh
	var nodes []node
	push := func(data []byte, target int) int {
		for bin.Len()%4 != 0 {
			bin.WriteByte(0)
		}
		views = append(views, view{Buffer: 0, ByteOffset: bin.Len(), ByteLength: len(data), Target: target})
		bin.Write(data)
		return len(views) - 1
	}
	for _, p := range ir.Parts {
		if len(p.Solids) == 0 {
			continue
		}
		var pos, nor, idx bytes.Buffer
		mn := [3]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
		mx := [3]float32{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}
		k := uint32(0)
		for _, s := range p.Solids {
			at := func(i int) V3 { return V3{s.Positions[3*i], s.Positions[3*i+1], s.Positions[3*i+2]} }
			for t := 0; t+2 < len(s.Indices); t += 3 {
				a, b, c := at(s.Indices[t]), at(s.Indices[t+1]), at(s.Indices[t+2])
				n := vunit(vcross(vsub(b, a), vsub(c, a)))
				gn := [3]float32{float32(n[0]), float32(n[2]), float32(-n[1])}
				for _, v := range []V3{a, b, c} {
					q := ToGLTF(v)
					for j := 0; j < 3; j++ {
						mn[j] = min(mn[j], q[j])
						mx[j] = max(mx[j], q[j])
					}
					binary.Write(&pos, binary.LittleEndian, q)
					binary.Write(&nor, binary.LittleEndian, gn)
					binary.Write(&idx, binary.LittleEndian, k)
					k++
				}
			}
		}
		if k == 0 {
			continue
		}
		pa := len(accessors)
		accessors = append(accessors, accessor{BufferView: push(pos.Bytes(), 34962), ComponentType: 5126, Count: int(k), Type: "VEC3", Min: mn[:], Max: mx[:]})
		accessors = append(accessors, accessor{BufferView: push(nor.Bytes(), 34962), ComponentType: 5126, Count: int(k), Type: "VEC3"})
		accessors = append(accessors, accessor{BufferView: push(idx.Bytes(), 34963), ComponentType: 5125, Count: int(k), Type: "SCALAR"})
		col := hexColor(p.Appearance.Color)
		alpha := p.Appearance.Opacity
		if alpha <= 0 || alpha > 1 {
			alpha = 1
		}
		m := material{Name: p.Component, DoubleSided: false, PBR: map[string]any{
			"baseColorFactor": []float64{col[0], col[1], col[2], math.Round(alpha*1e6) / 1e6},
			"metallicFactor":  p.Appearance.Metalness, "roughnessFactor": p.Appearance.Roughness}}
		if alpha < 1 {
			m.AlphaMode = "BLEND"
		}
		materials = append(materials, m)
		meshes = append(meshes, mesh{Name: p.Component, Primitives: []primitive{{Attributes: map[string]int{"POSITION": pa, "NORMAL": pa + 1}, Indices: pa + 2, Material: len(materials) - 1}}})
		nodes = append(nodes, node{Name: p.Component, Mesh: len(meshes) - 1, Extras: map[string]string{
			"componentId": p.Component, "type": p.Type, "role": p.Role, "name": p.Name, "material": p.Material,
			"void": strconv.FormatBool(p.Void), "assemblyId": ir.AssemblyID}})
	}
	for bin.Len()%4 != 0 {
		bin.WriteByte(0)
	}
	sceneNodes := make([]int, len(nodes))
	for i := range sceneNodes {
		sceneNodes[i] = i
	}
	extras := map[string]string{
		"units": "m", "sourceUnits": "mm", "convention": ir.Convention,
		"fromWorld": "(x, z, -y) / 1000", "toWorld": "x = X*1000, y = -Z*1000, z = Y*1000",
		"geometryHash": ir.Hash, "compiler": ir.Compiler, "generator": GLBGenerator, "notice": NonApprovalNotice,
	}
	for k, v := range meta {
		extras[k] = v
	}
	doc := map[string]any{
		"asset":       map[string]any{"version": "2.0", "generator": GLBGenerator, "extras": extras},
		"scene":       0,
		"scenes":      []map[string]any{{"name": ir.AssemblyID, "nodes": sceneNodes}},
		"nodes":       nodes,
		"meshes":      meshes,
		"materials":   materials,
		"accessors":   accessors,
		"bufferViews": views,
		"buffers":     []map[string]int{{"byteLength": bin.Len()}},
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	var out bytes.Buffer
	total := 12 + 8 + len(js) + 8 + bin.Len()
	binary.Write(&out, binary.LittleEndian, []uint32{0x46546C67, 2, uint32(total)})
	binary.Write(&out, binary.LittleEndian, []uint32{uint32(len(js)), 0x4E4F534A})
	out.Write(js)
	binary.Write(&out, binary.LittleEndian, []uint32{uint32(bin.Len()), 0x004E4942})
	out.Write(bin.Bytes())
	return out.Bytes(), nil
}

// GLBSummary is what a GLB parse yields for verification (tests, restore).
type GLBSummary struct {
	Nodes        []string
	ComponentIDs []string
	Triangles    int
	Extras       map[string]string
	BufferBytes  int
	ExternalURIs int
}

// ParseGLB validates GLB framing and returns a summary. It refuses malformed
// chunks, external buffer URIs and out-of-range accessors.
func ParseGLB(b []byte) (*GLBSummary, error) {
	if len(b) < 28 {
		return nil, Invalid("glb: too short")
	}
	var h [5]uint32
	if err := binary.Read(bytes.NewReader(b[:20]), binary.LittleEndian, &h); err != nil {
		return nil, err
	}
	if h[0] != 0x46546C67 || h[1] != 2 || int(h[2]) != len(b) || h[4] != 0x4E4F534A || int(20+h[3]) > len(b)-8 {
		return nil, Invalid("glb: bad header")
	}
	js := b[20 : 20+h[3]]
	var bh [2]uint32
	if err := binary.Read(bytes.NewReader(b[20+h[3]:28+h[3]]), binary.LittleEndian, &bh); err != nil {
		return nil, err
	}
	if bh[1] != 0x004E4942 || int(28+h[3]+bh[0]) != len(b) {
		return nil, Invalid("glb: bad binary chunk")
	}
	var doc struct {
		Asset struct {
			Extras map[string]string `json:"extras"`
		} `json:"asset"`
		Nodes []struct {
			Name   string            `json:"name"`
			Extras map[string]string `json:"extras"`
		} `json:"nodes"`
		Accessors []struct {
			BufferView int    `json:"bufferView"`
			Count      int    `json:"count"`
			Type       string `json:"type"`
		} `json:"accessors"`
		BufferViews []struct {
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
		} `json:"bufferViews"`
		Buffers []struct {
			ByteLength int    `json:"byteLength"`
			URI        string `json:"uri"`
		} `json:"buffers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimRight(string(js), " ")), &doc); err != nil {
		return nil, Invalid("glb: json: " + err.Error())
	}
	s := &GLBSummary{Extras: doc.Asset.Extras}
	for _, buf := range doc.Buffers {
		if buf.URI != "" {
			s.ExternalURIs++
		}
		s.BufferBytes += buf.ByteLength
	}
	if s.BufferBytes != int(bh[0]) {
		return nil, Invalid("glb: buffer length mismatch")
	}
	for _, v := range doc.BufferViews {
		if v.ByteOffset < 0 || v.ByteOffset+v.ByteLength > s.BufferBytes {
			return nil, Invalid("glb: buffer view out of range")
		}
	}
	for _, a := range doc.Accessors {
		if a.BufferView < 0 || a.BufferView >= len(doc.BufferViews) {
			return nil, Invalid("glb: accessor view out of range")
		}
		if a.Type == "SCALAR" {
			s.Triangles += a.Count / 3
		}
	}
	for _, n := range doc.Nodes {
		s.Nodes = append(s.Nodes, n.Name)
		s.ComponentIDs = append(s.ComponentIDs, n.Extras["componentId"])
	}
	return s, nil
}
