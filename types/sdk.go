package types

import (
	"time"

	"github.com/surrealdb/surrealdb.go/pkg/models"
)

// ToSDKValue converts driver-specific custom types into their SurrealDB SDK 1.4
// counterparts so that CBOR serialization works correctly.
// Pointers are returned because the SDK's MarshalCBOR methods have pointer receivers.
func ToSDKValue(v interface{}) interface{} {
	switch val := v.(type) {
	case time.Time:
		return &models.CustomDateTime{Time: val}
	case *time.Time:
		if val == nil {
			return nil
		}
		return &models.CustomDateTime{Time: *val}
	case DeletedAt:
		if val.Valid {
			return &models.CustomDateTime{Time: val.Time}
		}
		return &models.CustomNil{}
	case *DeletedAt:
		if val == nil || !val.Valid {
			return &models.CustomNil{}
		}
		return &models.CustomDateTime{Time: val.Time}
	case Duration:
		return &models.CustomDuration{Duration: val.Duration}
	case *Duration:
		if val == nil {
			return nil
		}
		return &models.CustomDuration{Duration: val.Duration}
	case GeometryPoint:
		if len(val.Coordinates) == 2 {
			return &models.GeometryPoint{Longitude: val.Coordinates[0], Latitude: val.Coordinates[1]}
		}
		return nil
	case *GeometryPoint:
		if val == nil || len(val.Coordinates) != 2 {
			return nil
		}
		return &models.GeometryPoint{Longitude: val.Coordinates[0], Latitude: val.Coordinates[1]}
	case GeometryLine:
		line := make(models.GeometryLine, len(val.Coordinates))
		for i, c := range val.Coordinates {
			if len(c) == 2 {
				line[i] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
			}
		}
		return &line
	case *GeometryLine:
		if val == nil {
			return nil
		}
		line := make(models.GeometryLine, len(val.Coordinates))
		for i, c := range val.Coordinates {
			if len(c) == 2 {
				line[i] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
			}
		}
		return &line
	case GeometryPolygon:
		poly := make(models.GeometryPolygon, len(val.Coordinates))
		for i, ring := range val.Coordinates {
			line := make(models.GeometryLine, len(ring))
			for j, c := range ring {
				if len(c) == 2 {
					line[j] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
				}
			}
			poly[i] = line
		}
		return &poly
	case *GeometryPolygon:
		if val == nil {
			return nil
		}
		poly := make(models.GeometryPolygon, len(val.Coordinates))
		for i, ring := range val.Coordinates {
			line := make(models.GeometryLine, len(ring))
			for j, c := range ring {
				if len(c) == 2 {
					line[j] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
				}
			}
			poly[i] = line
		}
		return &poly
	case GeometryMultiPoint:
		mp := make(models.GeometryMultiPoint, len(val.Coordinates))
		for i, c := range val.Coordinates {
			if len(c) == 2 {
				mp[i] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
			}
		}
		return &mp
	case *GeometryMultiPoint:
		if val == nil {
			return nil
		}
		mp := make(models.GeometryMultiPoint, len(val.Coordinates))
		for i, c := range val.Coordinates {
			if len(c) == 2 {
				mp[i] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
			}
		}
		return &mp
	case GeometryMultiLineString:
		ml := make(models.GeometryMultiLine, len(val.Coordinates))
		for i, lineCoords := range val.Coordinates {
			line := make(models.GeometryLine, len(lineCoords))
			for j, c := range lineCoords {
				if len(c) == 2 {
					line[j] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
				}
			}
			ml[i] = line
		}
		return &ml
	case *GeometryMultiLineString:
		if val == nil {
			return nil
		}
		ml := make(models.GeometryMultiLine, len(val.Coordinates))
		for i, lineCoords := range val.Coordinates {
			line := make(models.GeometryLine, len(lineCoords))
			for j, c := range lineCoords {
				if len(c) == 2 {
					line[j] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
				}
			}
			ml[i] = line
		}
		return &ml
	case GeometryMultiPolygon:
		mp := make(models.GeometryMultiPolygon, len(val.Coordinates))
		for i, polyCoords := range val.Coordinates {
			poly := make(models.GeometryPolygon, len(polyCoords))
			for j, ring := range polyCoords {
				line := make(models.GeometryLine, len(ring))
				for k, c := range ring {
					if len(c) == 2 {
						line[k] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
					}
				}
				poly[j] = line
			}
			mp[i] = poly
		}
		return &mp
	case *GeometryMultiPolygon:
		if val == nil {
			return nil
		}
		mp := make(models.GeometryMultiPolygon, len(val.Coordinates))
		for i, polyCoords := range val.Coordinates {
			poly := make(models.GeometryPolygon, len(polyCoords))
			for j, ring := range polyCoords {
				line := make(models.GeometryLine, len(ring))
				for k, c := range ring {
					if len(c) == 2 {
						line[k] = models.GeometryPoint{Longitude: c[0], Latitude: c[1]}
					}
				}
				poly[j] = line
			}
			mp[i] = poly
		}
		return &mp
	case GeometryCollection:
		coll := make(models.GeometryCollection, len(val.Geometries))
		for i, g := range val.Geometries {
			coll[i] = ToSDKValue(g)
		}
		return &coll
	case *GeometryCollection:
		if val == nil {
			return nil
		}
		coll := make(models.GeometryCollection, len(val.Geometries))
		for i, g := range val.Geometries {
			coll[i] = ToSDKValue(g)
		}
		return &coll
	case RecordID:
		return &val.RecordID
	case *RecordID:
		if val == nil {
			return nil
		}
		return &val.RecordID
	case UUID:
		return val.String
	case *UUID:
		if val == nil {
			return nil
		}
		return val.String
	case Decimal:
		return val.String()
	case *Decimal:
		if val == nil {
			return nil
		}
		return val.String()
	case DateTime:
		return &models.CustomDateTime{Time: val.Time}
	case *DateTime:
		if val == nil {
			return nil
		}
		return &models.CustomDateTime{Time: val.Time}
	case Regex:
		return val.String()
	case *Regex:
		if val == nil {
			return nil
		}
		return val.String()
	case File:
		// File implements MarshalCBOR (tag 55), which surrealcbor honors.
		return val
	case *File:
		if val == nil {
			return nil
		}
		return *val
	}
	return v
}
