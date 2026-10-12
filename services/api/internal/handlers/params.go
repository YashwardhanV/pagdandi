package handlers

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"pagdandi/api/internal/geo"
	"pagdandi/api/internal/httpx"
)

func badParam(name, msg string) error {
	return httpx.Err(http.StatusBadRequest, "BAD_PARAM", name+": "+msg)
}

func floatParam(r *http.Request, name string, required bool) (*float64, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		if required {
			return nil, badParam(name, "required")
		}
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, badParam(name, "must be a number")
	}
	return &f, nil
}

func pointParam(r *http.Request, required bool) (*geo.Point, error) {
	lat, err := floatParam(r, "lat", required)
	if err != nil {
		return nil, err
	}
	lng, err := floatParam(r, "lng", required)
	if err != nil {
		return nil, err
	}
	if lat == nil || lng == nil {
		if lat != nil || lng != nil {
			return nil, badParam("lat/lng", "give both or neither")
		}
		return nil, nil
	}
	p := geo.Point{Lat: *lat, Lng: *lng}
	if !p.Valid() {
		return nil, badParam("lat/lng", "out of range")
	}
	return &p, nil
}

// bboxParam parses bbox=minLng,minLat,maxLng,maxLat and caps its area.
func bboxParam(r *http.Request, maxDeg float64) (geo.BBox, error) {
	parts := strings.Split(r.URL.Query().Get("bbox"), ",")
	if len(parts) != 4 {
		return geo.BBox{}, badParam("bbox", "expected minLng,minLat,maxLng,maxLat")
	}
	var b geo.BBox
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return geo.BBox{}, badParam("bbox", "must be numbers")
		}
		b[i] = f
	}
	if !b.Valid() {
		return geo.BBox{}, badParam("bbox", "invalid box")
	}
	if b[2]-b[0] > maxDeg || b[3]-b[1] > maxDeg {
		return geo.BBox{}, badParam("bbox", "area too large; zoom in")
	}
	return b, nil
}

func langParam(r *http.Request) string {
	switch l := r.URL.Query().Get("lang"); l {
	case "hi", "sv":
		return l
	default:
		return "en"
	}
}
