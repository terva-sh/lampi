package config

import "time"

// now is a var so a test can pin the clock without exporting a clock type
// the commands do not need.
var now = time.Now
