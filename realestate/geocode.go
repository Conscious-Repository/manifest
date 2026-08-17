package realestate

import "manifest/geocode"

// Geocoder remains as a compatibility alias for callers in the real-estate
// package. The implementation is app-wide so contacts and properties share a
// single provider limiter and cache.
type Geocoder = geocode.Service

var NewGeocoder = geocode.New
