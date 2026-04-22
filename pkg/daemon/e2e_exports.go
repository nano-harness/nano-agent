//go:build e2e

package daemon

import "github.com/gorilla/mux"

// SetupRoutesForTest exposes the private setupRoutes method for e2e tests.
// This allows test harnesses to create HTTP servers with the full daemon router.
func (ds *Server) SetupRoutesForTest() *mux.Router {
	return ds.setupRoutes()
}
