package storefront

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// A buyer from a company needs an invoice, so the requisites travel as the file
// they already have: twelve fields nobody retypes into a form. 5 MB is a scan of
// a certificate with room to spare.
const kMaxRequisitesSize = 5 << 20

// Kept in sync with pricelist/src/app/handler/validate.go: nine digits (УНП), or
// two capital Latin letters and seven digits for a foreign entity. A Russian ИНН
// is ten digits for a company and twelve for a sole trader; refusing it would
// lose exactly the buyer this whole feature exists to catch.
var orgIDRegex = regexp.MustCompile(`^([0-9]{9}|[0-9]{10}|[0-9]{12}|[A-Z]{2}[0-9]{7})$`)

// Scans and office documents, no archives and nothing executable.
var kRequisitesExt = map[string]bool{
	".pdf": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".odt": true,
}

// RequisitesDir sits beside the uploads directory rather than inside it, because
// everything under uploads is served to anyone who asks.
func RequisitesDir(uploadsDir string) string {
	return filepath.Join(filepath.Dir(uploadsDir), "requisites")
}

type orgForm struct {
	// Chosen: the shop sells only to companies, or the buyer ticked the box.
	Chosen bool
	Name   string
	UNP    string
}

// readOrgForm answers who is ordering. A shop that sells to private buyers only
// ignores the fields entirely, so a crafted POST cannot smuggle them in.
func readOrgForm(shop *database.Settings, r *http.Request) orgForm {
	if !shop.OrgOnly() && !shop.OrgChoice() {
		return orgForm{}
	}
	f := orgForm{
		Chosen: shop.OrgOnly() || r.FormValue("customer") == database.CustomerCompany,
		Name:   strings.TrimSpace(r.FormValue("org_name")),
		UNP:    strings.ToUpper(strings.TrimSpace(r.FormValue("org_unp"))),
	}
	if !f.Chosen {
		// The hidden inputs are posted anyway: display:none does not stop a form.
		return orgForm{}
	}
	return f
}

// Valid keeps the invariant that makes an empty OrgName mean "private buyer".
func (f orgForm) Valid() bool {
	if !f.Chosen {
		return true
	}
	return f.Name != "" && orgIDRegex.MatchString(f.UNP)
}

// orderMailBody is what the owner reads instead of opening the admin. Separate
// from the handler so the organisation lines can be checked without an SMTP server.
func orderMailBody(lang, summary, total string, o *database.Order, baseURL string) string {
	var b strings.Builder
	b.WriteString(summary)
	fmt.Fprintf(&b, "%s: %s\n\n", i18n.T(lang, i18n.KeyOrderTotal), total)
	fmt.Fprintf(&b, "%s: %s\n", i18n.T(lang, i18n.KeyOrderName), o.Name)
	fmt.Fprintf(&b, "%s: %s\n", i18n.T(lang, i18n.KeyOrderPhone), o.Phone)
	fmt.Fprintf(&b, "%s: %s\n", i18n.T(lang, i18n.KeyOrderEmail), o.Email)
	if o.OrgName != "" {
		fmt.Fprintf(&b, "%s: %s\n", i18n.T(lang, i18n.KeyOrderOrgName), o.OrgName)
		fmt.Fprintf(&b, "%s: %s\n", i18n.T(lang, i18n.KeyOrderOrgUNP), o.OrgUNP)
		if o.RequisitesFile != "" {
			fmt.Fprintf(&b, "%s: %s/admin\n", i18n.T(lang, i18n.KeyOrderRequisites), baseURL)
		}
	}
	fmt.Fprintf(&b, "%s: %s\n\n%s/admin", i18n.T(lang, i18n.KeyOrderComment), o.Comment, baseURL)
	return b.String()
}

// saveRequisites returns the stored file name, empty when nothing was attached.
// A missing file is not an error: the seller calls back either way.
func (s *Storefront) saveRequisites(r *http.Request) string {
	f, hdr, err := r.FormFile("requisites")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if !kRequisitesExt[ext] {
		return ""
	}
	if err := os.MkdirAll(s.requisites, 0700); err != nil {
		log.Warnf("requisites dir: %v", err)
		return ""
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	name := hex.EncodeToString(b[:]) + ext
	dst, err := os.Create(filepath.Join(s.requisites, name))
	if err != nil {
		log.Warnf("requisites file: %v", err)
		return ""
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, io.LimitReader(f, kMaxRequisitesSize)); err != nil {
		log.Warnf("requisites copy: %v", err)
		return ""
	}
	return name
}
