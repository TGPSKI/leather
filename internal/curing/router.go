package curing

import (
	"github.com/tgpski/leather/internal/model"
)

// Router performs first-match routing from an intake event to a TanneryRoute.
// Route order is preserved from the configuration slice.
type Router struct {
	routes []model.TanneryRoute
}

// NewRouter returns a Router backed by the provided routes.
func NewRouter(routes []model.TanneryRoute) *Router {
	return &Router{routes: routes}
}

// Match returns the first TanneryRoute whose Match.Source equals source and
// whose Match.EventType either is empty (wildcard) or equals eventType.
// Returns (zero, false) when no route matches.
func (r *Router) Match(source, eventType string) (model.TanneryRoute, bool) {
	for _, route := range r.routes {
		if route.Match.Source != source {
			continue
		}
		if route.Match.EventType == "" || route.Match.EventType == eventType {
			return route, true
		}
	}
	return model.TanneryRoute{}, false
}

// MatchAll returns all TanneryRoutes whose Match.Source equals source and
// whose Match.EventType either is empty (wildcard) or equals eventType.
// Order is preserved from configuration. Returns nil when no route matches.
func (r *Router) MatchAll(source, eventType string) []model.TanneryRoute {
	var matches []model.TanneryRoute
	for _, route := range r.routes {
		if route.Match.Source != source {
			continue
		}
		if route.Match.EventType == "" || route.Match.EventType == eventType {
			matches = append(matches, route)
		}
	}
	return matches
}

// Routes returns the ordered list of routes held by this Router.
func (r *Router) Routes() []model.TanneryRoute {
	out := make([]model.TanneryRoute, len(r.routes))
	copy(out, r.routes)
	return out
}
