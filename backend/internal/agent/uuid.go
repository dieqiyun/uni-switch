package agent

import "github.com/google/uuid"

func validUUID(s string) bool { _, e := uuid.Parse(s); return e == nil }
