package storefront

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The conversion goal is what a paid channel optimises on, so it must fire once
// per order and not once per view of the confirmation page: that address stays
// in history, and every reload would teach the ad system on an order that never
// happened.
const kGoalCookie = "order_goal"

// Short: the goal is read by the very next request, the redirect to /cart.
const kGoalTTL = 60

type orderGoal struct {
	ID    int64
	Total int64
}

func writeGoal(w http.ResponseWriter, id, total int64) {
	http.SetCookie(w, &http.Cookie{Name: kGoalCookie, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: kGoalTTL,
		Value: fmt.Sprintf("%d:%d", id, total)})
}

// takeGoal reads the goal and burns it in the same breath.
func takeGoal(w http.ResponseWriter, r *http.Request) *orderGoal {
	c, err := r.Cookie(kGoalCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: kGoalCookie, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
	id, total, ok := strings.Cut(c.Value, ":")
	if !ok {
		return nil
	}
	g := &orderGoal{}
	if g.ID, err = strconv.ParseInt(id, 10, 64); err != nil {
		return nil
	}
	if g.Total, err = strconv.ParseInt(total, 10, 64); err != nil {
		return nil
	}
	return g
}

// A number, not a string: html/template quotes a string in a script, and both
// Metrika and GA4 read a quoted price as no price at all. In the shop's own
// currency, not in minor units.
func (g *orderGoal) Value() float64 { return float64(g.Total) / 100 }

func (g *orderGoal) IDStr() string { return strconv.FormatInt(g.ID, 10) }
