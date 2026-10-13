package sun

import (
	"math"
	"time"
)

// Sunrise/sunset following the NOAA solar calculator (general solar position
// equations). Accurate to about a minute between ±65° latitude, which covers
// every Pagdandi region (Stockholm is 59.3°N).

// SunTimes returns sunrise and sunset (UTC) for the calendar day of `day` at
// the given location. ok is false during polar day/night.
func SunTimes(day time.Time, lat, lng float64) (sunrise, sunset time.Time, ok bool) {
	y, m, d := day.UTC().Date()
	date := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	// Julian day at 0h UTC, then Julian century.
	jd := float64(date.Unix())/86400.0 + 2440587.5
	noon := solarNoonMinutes(jd, lng)
	ha, ok := hourAngleSunrise(jd+noon/1440, lat)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	// Iterate once around the approximate times for better accuracy.
	riseMin := noon - 4*ha
	setMin := noon + 4*ha
	if ha2, ok2 := hourAngleSunrise(jd+riseMin/1440, lat); ok2 {
		riseMin = solarNoonMinutes(jd+riseMin/1440, lng) - 4*ha2
	}
	if ha2, ok2 := hourAngleSunrise(jd+setMin/1440, lat); ok2 {
		setMin = solarNoonMinutes(jd+setMin/1440, lng) + 4*ha2
	}
	return date.Add(time.Duration(riseMin * float64(time.Minute))), date.Add(time.Duration(setMin * float64(time.Minute))), true
}

// IsDaylight reports whether the sun is up at t for the location. During polar
// day it returns true, during polar night false.
func IsDaylight(t time.Time, lat, lng float64) bool {
	// Use the local solar day so that late-evening UTC times map to the right date.
	localSolar := t.UTC().Add(time.Duration(lng / 15 * float64(time.Hour)))
	rise, set, ok := SunTimes(localSolar, lat, lng)
	if !ok {
		jd := float64(t.Unix())/86400.0 + 2440587.5
		decl := sunDeclination(julianCentury(jd))
		return lat*decl > 0 // same hemisphere as the sun → polar day
	}
	return !t.Before(rise) && t.Before(set)
}

func julianCentury(jd float64) float64 { return (jd - 2451545.0) / 36525.0 }

func degToRad(d float64) float64 { return d * math.Pi / 180 }
func radToDeg(r float64) float64 { return r * 180 / math.Pi }

func geomMeanLongSun(t float64) float64 {
	l := 280.46646 + t*(36000.76983+0.0003032*t)
	return math.Mod(math.Mod(l, 360)+360, 360)
}

func geomMeanAnomalySun(t float64) float64 { return 357.52911 + t*(35999.05029-0.0001537*t) }

func eccentricityEarthOrbit(t float64) float64 { return 0.016708634 - t*(0.000042037+0.0000001267*t) }

func sunEqOfCenter(t float64) float64 {
	m := degToRad(geomMeanAnomalySun(t))
	return math.Sin(m)*(1.914602-t*(0.004817+0.000014*t)) + math.Sin(2*m)*(0.019993-0.000101*t) + math.Sin(3*m)*0.000289
}

func sunApparentLong(t float64) float64 {
	o := geomMeanLongSun(t) + sunEqOfCenter(t)
	return o - 0.00569 - 0.00478*math.Sin(degToRad(125.04-1934.136*t))
}

func obliquityCorrection(t float64) float64 {
	e0 := 23 + (26+(21.448-t*(46.815+t*(0.00059-t*0.001813)))/60)/60
	return e0 + 0.00256*math.Cos(degToRad(125.04-1934.136*t))
}

func sunDeclination(t float64) float64 {
	return radToDeg(math.Asin(math.Sin(degToRad(obliquityCorrection(t))) * math.Sin(degToRad(sunApparentLong(t)))))
}

func equationOfTime(t float64) float64 {
	eps := obliquityCorrection(t)
	l0 := degToRad(geomMeanLongSun(t))
	e := eccentricityEarthOrbit(t)
	m := degToRad(geomMeanAnomalySun(t))
	y := math.Tan(degToRad(eps) / 2)
	y *= y
	eq := y*math.Sin(2*l0) - 2*e*math.Sin(m) + 4*e*y*math.Sin(m)*math.Cos(2*l0) - 0.5*y*y*math.Sin(4*l0) - 1.25*e*e*math.Sin(2*m)
	return radToDeg(eq) * 4 // minutes
}

// solarNoonMinutes returns minutes after 0h UTC of solar noon on the day starting at jd.
func solarNoonMinutes(jd, lng float64) float64 {
	day := math.Floor(jd-0.5) + 0.5
	t := julianCentury(day - lng/360 + 0.5)
	return 720 - 4*lng - equationOfTime(t)
}

// hourAngleSunrise returns the hour angle (degrees) of sunrise for the given moment.
func hourAngleSunrise(jd, lat float64) (float64, bool) {
	t := julianCentury(jd)
	decl := degToRad(sunDeclination(t))
	latR := degToRad(lat)
	cosHA := math.Cos(degToRad(90.833))/(math.Cos(latR)*math.Cos(decl)) - math.Tan(latR)*math.Tan(decl)
	if cosHA < -1 || cosHA > 1 {
		return 0, false
	}
	return radToDeg(math.Acos(cosHA)), true
}
