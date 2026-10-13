// Package raster is a byte grid over the region: each cell holds bit flags
// describing the land under it (water, forest, built-up …). datactl paints it
// from OpenStreetMap; the API reads it to score routes and to steer jetskis.
//
// One cell is ~50 m. The Indore circle (250 km across) is ~5000×5000 cells,
// 25 MB in memory and a few MB gzipped on disk. Lookups are O(1).
package raster

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"pagdandi/api/internal/geo"
)

// Cell flags.
const (
	Water     byte = 1 << iota // lake, reservoir or river area (jetski-able)
	NearWater                  // within ~200 m of water or a river line
	Forest                     // forest or wood
	BuiltUp                    // residential, commercial, industrial, retail
	Green                      // park, meadow, grassland, orchard
	Protected                  // nature reserve, protected area, national park
	River                      // river centre line or river area
	Farmland                   // fields
)

const magic = "PGR1"

// Grid is a lat/lng-aligned byte raster.
type Grid struct {
	W, H           int
	MinLng, MinLat float64
	DLng, DLat     float64 // cell size in degrees
	Cells          []byte
}

// New allocates a grid covering b with roughly cellM-metre cells.
func New(b geo.BBox, cellM float64) *Grid {
	midLat := (b[1] + b[3]) / 2
	dLat := cellM / 111320.0
	dLng := cellM / (111320.0 * math.Cos(midLat*math.Pi/180))
	w := int(math.Ceil((b[2] - b[0]) / dLng))
	h := int(math.Ceil((b[3] - b[1]) / dLat))
	return &Grid{W: w, H: h, MinLng: b[0], MinLat: b[1], DLng: dLng, DLat: dLat, Cells: make([]byte, w*h)}
}

// CellM is the approximate cell edge in metres.
func (g *Grid) CellM() float64 { return g.DLat * 111320.0 }

// XY returns the cell containing p.
func (g *Grid) XY(p geo.Point) (x, y int, ok bool) {
	x = int(math.Floor((p.Lng - g.MinLng) / g.DLng))
	y = int(math.Floor((p.Lat - g.MinLat) / g.DLat))
	return x, y, x >= 0 && y >= 0 && x < g.W && y < g.H
}

// Center returns the centre of cell (x, y).
func (g *Grid) Center(x, y int) geo.Point {
	return geo.Point{Lat: g.MinLat + (float64(y)+0.5)*g.DLat, Lng: g.MinLng + (float64(x)+0.5)*g.DLng}
}

// At returns the flags at p (0 outside the grid).
func (g *Grid) At(p geo.Point) byte {
	x, y, ok := g.XY(p)
	if !ok {
		return 0
	}
	return g.Cells[y*g.W+x]
}

// Get returns the flags of cell (x, y) (0 outside).
func (g *Grid) Get(x, y int) byte {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return 0
	}
	return g.Cells[y*g.W+x]
}

// FillPolygon sets flag on every cell whose centre is inside the polygon
// (even-odd rule over all rings, so holes stay empty).
func (g *Grid) FillPolygon(rings [][]geo.Point, flag byte) int {
	type edge struct{ x0, y0, x1, y1 float64 }
	var edges []edge
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, r := range rings {
		for i := 0; i+1 < len(r); i++ {
			a, b := r[i], r[i+1]
			e := edge{(a.Lng - g.MinLng) / g.DLng, (a.Lat - g.MinLat) / g.DLat, (b.Lng - g.MinLng) / g.DLng, (b.Lat - g.MinLat) / g.DLat}
			if e.y0 == e.y1 {
				continue
			}
			edges = append(edges, e)
			minY, maxY = math.Min(minY, math.Min(e.y0, e.y1)), math.Max(maxY, math.Max(e.y0, e.y1))
		}
	}
	if len(edges) == 0 {
		return 0
	}
	y0 := int(math.Max(0, math.Floor(minY)))
	y1 := int(math.Min(float64(g.H-1), math.Ceil(maxY)))
	n := 0
	xs := make([]float64, 0, 16)
	for y := y0; y <= y1; y++ {
		cy := float64(y) + 0.5
		xs = xs[:0]
		for _, e := range edges {
			if (e.y0 <= cy) != (e.y1 <= cy) {
				xs = append(xs, e.x0+(cy-e.y0)*(e.x1-e.x0)/(e.y1-e.y0))
			}
		}
		sort.Float64s(xs)
		row := y * g.W
		for i := 0; i+1 < len(xs); i += 2 {
			from := int(math.Max(0, math.Ceil(xs[i]-0.5)))
			to := int(math.Min(float64(g.W-1), math.Floor(xs[i+1]-0.5)))
			for x := from; x <= to; x++ {
				g.Cells[row+x] |= flag
				n++
			}
		}
	}
	return n
}

// DrawLine sets flag along a polyline, widened by halfWidth cells on each side.
func (g *Grid) DrawLine(line []geo.Point, flag byte, halfWidth int) {
	for i := 0; i+1 < len(line); i++ {
		ax, ay := (line[i].Lng-g.MinLng)/g.DLng, (line[i].Lat-g.MinLat)/g.DLat
		bx, by := (line[i+1].Lng-g.MinLng)/g.DLng, (line[i+1].Lat-g.MinLat)/g.DLat
		steps := int(math.Ceil(math.Max(math.Abs(bx-ax), math.Abs(by-ay))*2)) + 1
		for s := 0; s <= steps; s++ {
			t := float64(s) / float64(steps)
			cx, cy := int(math.Floor(ax+(bx-ax)*t)), int(math.Floor(ay+(by-ay)*t))
			for dy := -halfWidth; dy <= halfWidth; dy++ {
				for dx := -halfWidth; dx <= halfWidth; dx++ {
					x, y := cx+dx, cy+dy
					if x >= 0 && y >= 0 && x < g.W && y < g.H {
						g.Cells[y*g.W+x] |= flag
					}
				}
			}
		}
	}
}

// Dilate sets dst on every cell within r cells (Chebyshev) of a cell having any of src.
func (g *Grid) Dilate(src, dst byte, r int) {
	tmp := make([]bool, len(g.Cells))
	// Horizontal pass: running count of src cells inside the window.
	for y := 0; y < g.H; y++ {
		row := y * g.W
		count := 0
		for x := -r; x < g.W; x++ {
			if in := x + r; in < g.W && g.Cells[row+in]&src != 0 {
				count++
			}
			if out := x - r - 1; out >= 0 && g.Cells[row+out]&src != 0 {
				count--
			}
			if x >= 0 && count > 0 {
				tmp[row+x] = true
			}
		}
	}
	// Vertical pass over the horizontal result.
	for x := 0; x < g.W; x++ {
		count := 0
		for y := -r; y < g.H; y++ {
			if in := y + r; in < g.H && tmp[in*g.W+x] {
				count++
			}
			if out := y - r - 1; out >= 0 && tmp[out*g.W+x] {
				count--
			}
			if y >= 0 && count > 0 {
				g.Cells[y*g.W+x] |= dst
			}
		}
	}
}

// Count returns how many cells have flag set.
func (g *Grid) Count(flag byte) int {
	n := 0
	for _, c := range g.Cells {
		if c&flag != 0 {
			n++
		}
	}
	return n
}

type header struct {
	W, H                       int32
	MinLng, MinLat, DLng, DLat float64
}

// Write saves the grid gzipped.
func (g *Grid) Write(path string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	bw := bufio.NewWriter(zw)
	if _, err := bw.WriteString(magic); err != nil {
		f.Close()
		return err
	}
	h := header{int32(g.W), int32(g.H), g.MinLng, g.MinLat, g.DLng, g.DLat}
	if err := binary.Write(bw, binary.LittleEndian, h); err != nil {
		f.Close()
		return err
	}
	if _, err := bw.Write(g.Cells); err != nil {
		f.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read loads a grid written by Write.
func Read(path string) (*Grid, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	m := make([]byte, 4)
	if _, err := io.ReadFull(zr, m); err != nil || string(m) != magic {
		return nil, errors.New("raster: not a Pagdandi grid")
	}
	var h header
	if err := binary.Read(zr, binary.LittleEndian, &h); err != nil {
		return nil, err
	}
	if h.W <= 0 || h.H <= 0 || int64(h.W)*int64(h.H) > 400_000_000 {
		return nil, fmt.Errorf("raster: bad size %dx%d", h.W, h.H)
	}
	g := &Grid{W: int(h.W), H: int(h.H), MinLng: h.MinLng, MinLat: h.MinLat, DLng: h.DLng, DLat: h.DLat}
	g.Cells = make([]byte, g.W*g.H)
	if _, err := io.ReadFull(zr, g.Cells); err != nil {
		return nil, fmt.Errorf("raster: %w", err)
	}
	return g, nil
}
