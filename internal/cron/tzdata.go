package cron

// Embed the IANA tz database so DST-safe in-zone recompute (D-07) works on hosts
// that lack /usr/share/zoneinfo (e.g. a scratch container). The blank import
// registers the embedded database with time.LoadLocation at init.
import _ "time/tzdata"
