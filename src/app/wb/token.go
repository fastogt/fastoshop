package wb

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Bits of the token's `s` mask, checked on live tokens; the rest are counted, never named.
var kTokenSections = []struct {
	bit uint
	key string
}{
	{1, "content"}, {3, "prices"}, {4, "marketplace"},
	{5, "statistics"}, {2, "analytics"}, {13, "finance"},
}

// `acc` in the token: what WB's cabinet calls the token type.
var kTokenKinds = map[int]string{1: "basic", 3: "personal"}

// tokenInfo is what the stored token says about itself; never the token.
type tokenInfo struct {
	Sections  []string   `json:"sections"`
	Unknown   int        `json:"unknown"`
	Kind      string     `json:"kind"`
	ExpiresAt *time.Time `json:"expires_at"`
	Sandbox   bool       `json:"sandbox"`
}

type tokenClaims struct {
	S   uint64 `json:"s"`
	Exp int64  `json:"exp"`
	T   bool   `json:"t"`
	Acc int    `json:"acc"`
}

// describeToken reads the public half of our own stored JWT; no signature check is needed.
func describeToken(token string) *tokenInfo {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var c tokenClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	info := &tokenInfo{Sections: []string{}, Kind: kTokenKinds[c.Acc], Sandbox: c.T}
	rest := c.S
	for _, sec := range kTokenSections {
		if c.S&(1<<sec.bit) != 0 {
			info.Sections = append(info.Sections, sec.key)
			rest &^= 1 << sec.bit
		}
	}
	for ; rest != 0; rest &= rest - 1 {
		info.Unknown++
	}
	if c.Exp > 0 {
		exp := time.Unix(c.Exp, 0).UTC()
		info.ExpiresAt = &exp
	}
	return info
}
