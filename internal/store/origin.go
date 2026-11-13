package store

import "github.com/jenmud/gorun/internal/origin"

// Origin is who started a run, from where, and with what (see the origin package).
type Origin = origin.Origin

// GitInfo is the state of the repository a run's Gorunfile is in.
type GitInfo = origin.GitInfo
