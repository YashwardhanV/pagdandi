// Package geo holds the small amount of spherical geometry the API needs:
// haversine distances, polyline coding, point-to-line projection and the
// ellipse filter used for community-shortcut candidates.
package geo

import (
	"errors"
	"math"
	"strings"
)

// EarthRadiusM is the mean Earth radius in metres.
const EarthRadiusM = 6371008.8

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Valid reports whether p is a finite, in-range coordinate.
func (p Point) Valid() bool {
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lng) || math.IsInf(p.Lat, 0) || math.IsInf(p.Lng, 0) {
		return false
	}
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

func rad(d float64) float64 { return d * math.Pi / 180 }

// Haversine returns the great-circle distance between a and b in metres.
func Haversine(a, b Point) float64 {
	dLat := rad(b.Lat - a.Lat)
	dLng := rad(b.Lng - a.Lng)
	s := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthRadiusM * math.Asin(math.Min(1, math.Sqrt(s)))
}

// Length returns the length of a polyline in metres.
func Length(line []Point) float64 {
	var d float64
	for i := 1; i < len(line); i++ {
		d += Haversine(line[i-1], line[i])
	}
	return d
}

// CumulativeDistances returns, for each vertex, the distance along the line from the start.
func CumulativeDistances(line []Point) []float64 {
	out := make([]float64, len(line))
	for i := 1; i < len(line); i++ {
		out[i] = out[i-1] + Haversine(line[i-1], line[i])
	}
	return out
}

// InEllipse reports whether p lies inside the ellipse with foci f1, f2 and the
// given major-axis length (metres): dist(p,f1)+dist(p,f2) <= major.
func InEllipse(p, f1, f2 Point, major float64) bool {
	return Haversine(p, f1)+Haversine(p, f2) <= major
}

// local projects p to metres on a plane tangent at origin (equirectangular).
// Accurate to well under a metre for the few-km spans it is used for.
func local(origin, p Point) (x, y float64) {
	x = rad(p.Lng-origin.Lng) * math.Cos(rad(origin.Lat)) * EarthRadiusM
	y = rad(p.Lat-origin.Lat) * EarthRadiusM
	return
}

// Projection describes the closest point on a line to some query point.
type Projection struct {
	Point    Point   // closest point on the line
	DistM    float64 // distance from the query point to the line
	AlongM   float64 // distance along the line from its start to Point
	Segment  int     // index i of the segment [i, i+1] containing Point
	Fraction float64 // position within that segment, 0..1
}

// Project finds the point on line closest to p.
func Project(line []Point, p Point) Projection {
	best := Projection{DistM: math.Inf(1)}
	if len(line) == 0 {
		return best
	}
	if len(line) == 1 {
		return Projection{Point: line[0], DistM: Haversine(line[0], p)}
	}
	cum := 0.0
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		bx, by := local(a, b)
		px, py := local(a, p)
		segLen2 := bx*bx + by*by
		t := 0.0
		if segLen2 > 0 {
			t = (px*bx + py*by) / segLen2
			t = math.Max(0, math.Min(1, t))
		}
		q := Point{Lat: a.Lat + (b.Lat-a.Lat)*t, Lng: a.Lng + (b.Lng-a.Lng)*t}
		d := Haversine(q, p)
		segLen := Haversine(a, b)
		if d < best.DistM {
			best = Projection{Point: q, DistM: d, AlongM: cum + segLen*t, Segment: i, Fraction: t}
		}
		cum += segLen
	}
	return best
}

// Encode encodes points with the standard polyline algorithm at precision 5.
func Encode(line []Point) string {
	return EncodePrecision(line, 5)
}

// EncodePrecision encodes points with the polyline algorithm at the given precision.
func EncodePrecision(line []Point, precision int) string {
	factor := math.Pow(10, float64(precision))
	var sb strings.Builder
	var prevLat, prevLng int64
	for _, p := range line {
		lat := int64(math.Round(p.Lat * factor))
		lng := int64(math.Round(p.Lng * factor))
		encodeValue(&sb, lat-prevLat)
		encodeValue(&sb, lng-prevLng)
		prevLat, prevLng = lat, lng
	}
	return sb.String()
}

func encodeValue(sb *strings.Builder, v int64) {
	u := v << 1
	if v < 0 {
		u = ^u
	}
	for u >= 0x20 {
		sb.WriteByte(byte((0x20 | (u & 0x1f)) + 63))
		u >>= 5
	}
	sb.WriteByte(byte(u + 63))
}

// ErrBadPolyline is returned for malformed encoded polylines.
var ErrBadPolyline = errors.New("geo: malformed polyline")

// Decode decodes a precision-5 encoded polyline.
func Decode(s string) ([]Point, error) {
	return DecodePrecision(s, 5)
}

// DecodePrecision decodes an encoded polyline at the given precision.
func DecodePrecision(s string, precision int) ([]Point, error) {
	factor := math.Pow(10, float64(precision))
	var out []Point
	var lat, lng int64
	i := 0
	next := func() (int64, error) {
		var result int64
		var shift uint
		for {
			if i >= len(s) {
				return 0, ErrBadPolyline
			}
			b := int64(s[i]) - 63
			i++
			if b < 0 || b > 63 {
				return 0, ErrBadPolyline
			}
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
			if shift > 60 {
				return 0, ErrBadPolyline
			}
		}
		if result&1 != 0 {
			return ^(result >> 1), nil
		}
		return result >> 1, nil
	}
	for i < len(s) {
		dLat, err := next()
		if err != nil {
			return nil, err
		}
		dLng, err := next()
		if err != nil {
			return nil, err
		}
		lat += dLat
		lng += dLng
		out = append(out, Point{Lat: float64(lat) / factor, Lng: float64(lng) / factor})
	}
	return out, nil
}

// Simplify reduces a line with Douglas–Peucker using a tolerance in metres.
func Simplify(line []Point, tolM float64) []Point {
	if len(line) < 3 || tolM <= 0 {
		return line
	}
	keep := make([]bool, len(line))
	keep[0], keep[len(line)-1] = true, true
	var rec func(lo, hi int)
	rec = func(lo, hi int) {
		if hi <= lo+1 {
			return
		}
		seg := []Point{line[lo], line[hi]}
		maxD, idx := -1.0, -1
		for i := lo + 1; i < hi; i++ {
			if d := Project(seg, line[i]).DistM; d > maxD {
				maxD, idx = d, i
			}
		}
		if maxD > tolM {
			keep[idx] = true
			rec(lo, idx)
			rec(idx, hi)
		}
	}
	rec(0, len(line)-1)
	out := make([]Point, 0, len(line))
	for i, k := range keep {
		if k {
			out = append(out, line[i])
		}
	}
	return out
}

// Destination returns the point reached from p travelling distM metres on the
// given initial bearing (degrees clockwise from north).
func Destination(p Point, bearingDeg, distM float64) Point {
	δ := distM / EarthRadiusM
	θ := rad(bearingDeg)
	φ1, λ1 := rad(p.Lat), rad(p.Lng)
	φ2 := math.Asin(math.Sin(φ1)*math.Cos(δ) + math.Cos(φ1)*math.Sin(δ)*math.Cos(θ))
	λ2 := λ1 + math.Atan2(math.Sin(θ)*math.Sin(δ)*math.Cos(φ1), math.Cos(δ)-math.Sin(φ1)*math.Sin(φ2))
	return Point{Lat: φ2 * 180 / math.Pi, Lng: math.Mod(λ2*180/math.Pi+540, 360) - 180}
}

// Bearing returns the initial bearing from a to b in degrees [0,360).
func Bearing(a, b Point) float64 {
	φ1, φ2 := rad(a.Lat), rad(b.Lat)
	Δλ := rad(b.Lng - a.Lng)
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

// Circle returns a closed ring with n sides approximating a circle of radiusM around c.
// Coordinates are [lng, lat] pairs, ready for GeoJSON.
func Circle(c Point, radiusM float64, n int) [][2]float64 {
	ring := make([][2]float64, 0, n+1)
	for i := 0; i < n; i++ {
		p := Destination(c, float64(i)*360/float64(n), radiusM)
		ring = append(ring, [2]float64{p.Lng, p.Lat})
	}
	return append(ring, ring[0])
}

// Corridor returns a closed ring buffering the straight segment a→b by bufM on
// each side, with semicircular caps (8 points each).
func Corridor(a, b Point, bufM float64) [][2]float64 {
	brg := Bearing(a, b)
	var ring [][2]float64
	add := func(p Point) { ring = append(ring, [2]float64{p.Lng, p.Lat}) }
	// left side a→b, cap around b, right side b→a, cap around a.
	for i := 0; i <= 8; i++ {
		add(Destination(b, brg-90+float64(i)*180/8, bufM))
	}
	for i := 0; i <= 8; i++ {
		add(Destination(a, brg+90+float64(i)*180/8, bufM))
	}
	return append(ring, ring[0])
}

// BBox is [minLng, minLat, maxLng, maxLat].
type BBox [4]float64

// Contains reports whether p is inside the box.
func (b BBox) Contains(p Point) bool {
	return p.Lng >= b[0] && p.Lng <= b[2] && p.Lat >= b[1] && p.Lat <= b[3]
}

// Valid reports whether the box is well formed.
func (b BBox) Valid() bool {
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b[0] <= b[2] && b[1] <= b[3] && b[0] >= -180 && b[2] <= 180 && b[1] >= -90 && b[3] <= 90
}

// SliceAlong returns the part of line between distances fromM and toM along it.
func SliceAlong(line []Point, fromM, toM float64) []Point {
	if len(line) < 2 || toM <= fromM {
		return nil
	}
	cum := CumulativeDistances(line)
	interp := func(d float64) (Point, int) {
		for i := 1; i < len(line); i++ {
			if cum[i] >= d {
				seg := cum[i] - cum[i-1]
				t := 0.0
				if seg > 0 {
					t = (d - cum[i-1]) / seg
				}
				return Point{Lat: line[i-1].Lat + (line[i].Lat-line[i-1].Lat)*t, Lng: line[i-1].Lng + (line[i].Lng-line[i-1].Lng)*t}, i
			}
		}
		return line[len(line)-1], len(line)
	}
	start, si := interp(math.Max(0, fromM))
	end, ei := interp(math.Min(cum[len(cum)-1], toM))
	out := []Point{start}
	for i := si; i < ei && i < len(line); i++ {
		out = append(out, line[i])
	}
	return append(out, end)
}
