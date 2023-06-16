package pgxtypefaster

import (
	"strings"
)

var quoteArrayReplacer = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
