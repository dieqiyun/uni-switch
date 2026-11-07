package configuration

import "encoding/json"

func prettyJSON(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) + "\n" }
