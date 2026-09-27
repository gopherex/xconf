package xconf

// Decoder converts one document into an object and optional JSON-pointer locations.
// Decoders preserve numeric precision and do not apply schema defaults or validation.
// Each call must return independently owned data.
type Decoder func([]byte) (map[string]any, map[string]Location, error)
