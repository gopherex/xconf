// Package app demonstrates an application schema that composes an external
// library schema (redislib.ConfigSchema).
package app

//go:generate xconfgen -type AppConfig

import (
	"github.com/gopherex/xconf"
	"github.com/gopherex/xconf/example/redislib"
	"github.com/gopherex/xconf/pkg/validate"
)

// Schema is the application-level configuration. xconfgen would consume this
// and emit AppConfig + Load() in a sibling file.
// Env names auto-derive: Port -> PORT, AllowedHosts -> ALLOWED_HOSTS,
// Redis embed -> REDIS_ADDR, REDIS_PASSWORD, REDIS_DB, REDIS_TIMEOUT.
// Explicit .Env(...) still overrides (see DSN -> DB_DSN).
var Schema = xconf.Define("AppConfig",
	xconf.Int("Port").
		Default(8080).
		Validate(validate.Range(1, 65535)),

	xconf.String("DSN").
		Env("DB_DSN"). // explicit override
		Required().
		Validate(validate.NonEmpty()),

	xconf.Slice[string]("AllowedHosts").
		EnvSplit(",").
		Validate(validate.Each(validate.NonEmpty())),

	xconf.Map[string, int]("RateLimits").
		EnvSplit(",").KVSplit("=").
		Validate(validate.MapValues[string, int](validate.Positive[int]())),

	// External library embedded under name "Redis".
	// Codegen emits:  Redis redislib.Config
	xconf.Embed("Redis", redislib.ConfigSchema),
)
