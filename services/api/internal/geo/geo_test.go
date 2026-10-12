package geo

import (
	"math"
	"testing"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestHaversine(t *testing.T) {
	// Indore centre to Bilawali lake: roughly 4.5 km as the crow flies.
	indore := Point{22.7196, 75.8577}
	bilawali := Point{22.6800, 75.8520}
	d := Haversine(indore, bilawali)
	if d < 4000 || d > 5000 {
		t.Fatalf("unexpected distance %v", d)
	}
	if Haversine(indore, indore) != 0 {
		t.Fatal("self distance must be zero")
	}
	// One degree of latitude is ~111.2 km.
	if d := Haversine(Point{0, 0}, Point{1, 0}); !near(d, 111195, 50) {
		t.Fatalf("1deg lat = %v", d)
	}
}

func TestPolylineRoundTrip(t *testing.T) {
	// Reference vector from the polyline algorithm documentation.
	in := []Point{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	enc := Encode(in)
	if enc != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Fatalf("encode = %q", enc)
	}
	out, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if !near(in[i].Lat, out[i].Lat, 1e-5) || !near(in[i].Lng, out[i].Lng, 1e-5) {
			t.Fatalf("point %d: %v != %v", i, in[i], out[i])
		}
	}
	if _, err := Decode("_p~iF~ps|U_ulL"); err == nil {
		t.Fatal("expected error on truncated input")
	}
}

func TestProject(t *testing.T) {
	line := []Point{{0, 0}, {0, 0.01}, {0.01, 0.01}}
	p := Project(line, Point{0.001, 0.005})
	if p.Segment != 0 || !near(p.Fraction, 0.5, 1e-3) {
		t.Fatalf("segment/fraction = %d/%v", p.Segment, p.Fraction)
	}
	if !near(p.DistM, 111.2, 1) {
		t.Fatalf("dist = %v", p.DistM)
	}
	if !near(p.AlongM, 556, 2) {
		t.Fatalf("along = %v", p.AlongM)
	}
	// Past the end: clamps to the last vertex.
	end := Project(line, Point{0.02, 0.01})
	if end.Segment != 1 || !near(end.Fraction, 1, 1e-9) {
		t.Fatalf("end projection = %+v", end)
	}
}

func TestInEllipse(t *testing.T) {
	o, d := Point{0, 0}, Point{0, 0.1}
	base := Haversine(o, d)
	if !InEllipse(Point{0, 0.05}, o, d, 1.25*base) {
		t.Fatal("midpoint must be inside")
	}
	if InEllipse(Point{0.1, 0.05}, o, d, 1.25*base) {
		t.Fatal("far point must be outside")
	}
}

func TestCircleAndCorridor(t *testing.T) {
	c := Point{22.7196, 75.8577}
	ring := Circle(c, 150000, 64)
	if len(ring) != 65 || ring[0] != ring[64] {
		t.Fatalf("ring must be closed with 65 coords, got %d", len(ring))
	}
	for _, xy := range ring[:64] {
		if d := Haversine(c, Point{xy[1], xy[0]}); !near(d, 150000, 1) {
			t.Fatalf("vertex at %v m", d)
		}
	}
	cor := Corridor(Point{28.6139, 77.2090}, Point{27.1751, 78.0421}, 20000)
	if cor[0] != cor[len(cor)-1] {
		t.Fatal("corridor ring must be closed")
	}
}

func TestSimplifyAndSlice(t *testing.T) {
	line := []Point{{0, 0}, {0, 0.001}, {0.00001, 0.002}, {0, 0.003}, {0, 0.004}}
	s := Simplify(line, 5)
	if len(s) != 2 {
		t.Fatalf("expected straight line to simplify to 2 points, got %d", len(s))
	}
	total := Length(line)
	part := SliceAlong(line, 100, 300)
	if l := Length(part); !near(l, 200, 1) {
		t.Fatalf("slice length %v (total %v)", l, total)
	}
}

func TestBBox(t *testing.T) {
	b := BBox{75, 22, 76, 23}
	if !b.Valid() || !b.Contains(Point{22.5, 75.5}) || b.Contains(Point{21, 75.5}) {
		t.Fatal("bbox checks failed")
	}
	if (BBox{76, 22, 75, 23}).Valid() {
		t.Fatal("inverted bbox must be invalid")
	}
	if (Point{math.NaN(), 0}).Valid() || (Point{91, 0}).Valid() {
		t.Fatal("invalid points accepted")
	}
}
