package wb

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func fakeToken(claims string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"ES256"}`)) + "." + enc([]byte(claims)) + ".sig"
}

// A live pilot token: s=8254 is content, analytics, prices, marketplace, statistics, finance.
func TestDescribeToken(t *testing.T) {
	info := describeToken(fakeToken(`{"s":8254,"exp":1806623175,"t":false,"acc":3}`))
	if info == nil {
		t.Fatal("a valid token was not read")
	}
	if got := strings.Join(info.Sections, ","); got != "content,prices,marketplace,statistics,analytics,finance" {
		t.Errorf("sections %q", got)
	}
	if info.Unknown != 0 || info.Kind != "personal" || info.Sandbox {
		t.Errorf("unknown %d kind %q sandbox %v", info.Unknown, info.Kind, info.Sandbox)
	}
	if info.ExpiresAt == nil || !info.ExpiresAt.Equal(time.Unix(1806623175, 0)) {
		t.Errorf("expiry %v", info.ExpiresAt)
	}

	// No Marketplace bit, one bit nobody named, a basic sandbox token.
	noStock := describeToken(fakeToken(fmt.Sprintf(`{"s":%d,"acc":1,"t":true}`, 1<<1|1<<3|1<<6)))
	if got := strings.Join(noStock.Sections, ","); got != "content,prices" {
		t.Errorf("sections %q", got)
	}
	if noStock.Unknown != 1 || noStock.Kind != "basic" || !noStock.Sandbox || noStock.ExpiresAt != nil {
		t.Errorf("%+v", noStock)
	}

	for _, bad := range []string{"", "not-a-jwt", "a.!!!.c", fakeToken("[1,2]")} {
		if describeToken(bad) != nil {
			t.Errorf("garbage %q was read as a token", bad)
		}
	}
}

// The settings answer says what the token holds and never carries the token itself.
func TestSettingsShowTokenSectionsNotToken(t *testing.T) {
	h, _, _ := newTest(t)
	tok := fakeToken(`{"s":8254,"exp":1806623175,"acc":3}`)
	do(t, h, "PUT", "/settings", `{"token":"`+tok+`"}`)
	rec := do(t, h, "GET", "/settings", "")
	if strings.Contains(rec.Body.String(), tok) || strings.Contains(rec.Body.String(), ".sig") {
		t.Fatal("the settings answer leaked the token")
	}
	got := decode[settingsResponse](t, rec)
	if got.TokenInfo == nil || len(got.TokenInfo.Sections) != 6 {
		t.Fatalf("token info %+v", got.TokenInfo)
	}
}
