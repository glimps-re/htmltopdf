package css

import "unsafe"

func sizeof(t Token) uintptr { return unsafe.Sizeof(t) }
