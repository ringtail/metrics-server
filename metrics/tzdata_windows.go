package main

import (
	"os"
)

func init() {
	os.Setenv("ZONEINFO", "c://tz/tzdata.zip")
}
