package server

import "net/http"

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Trusted, same-origin JSON API (token-gated where noted).
	mux.HandleFunc("POST /api/upload", s.rateLimit(s.uploadLimiter, s.authToken(s.handleUpload)))
	mux.HandleFunc("GET /api/uploads", s.authToken(s.handleListUploads))
	mux.HandleFunc("DELETE /api/uploads/{slug}", s.authToken(s.handleDeleteUpload))
	mux.HandleFunc("POST /api/uploads/{slug}/visibility", s.authToken(s.handleSetVisibility))
	mux.HandleFunc("GET /api/uploads/{slug}/stats", s.authToken(s.handleStats))
	mux.HandleFunc("POST /api/tokens", s.authAdmin(s.handleCreateToken))
	mux.HandleFunc("GET /api/tokens", s.authAdmin(s.handleListTokens))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.authAdmin(s.handleDeleteToken))
	mux.HandleFunc("GET /api/settings", s.authAdmin(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.authAdmin(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/audit", s.authAdmin(s.handleAuditLog))
	mux.HandleFunc("GET /api/uploads/{slug}/export", s.authToken(s.handleExportUpload))
	mux.HandleFunc("DELETE /api/uploads-by-owner", s.authToken(s.handleDeleteAllByOwner))
	mux.HandleFunc("GET /api/auth/providers", s.handleAuthProviders)
	mux.HandleFunc("POST /api/cli/login/start", s.rateLimit(s.cliLoginLimiter, s.handleCLILoginStart))
	mux.HandleFunc("POST /api/cli/login/poll", s.rateLimit(s.cliLoginLimiter, s.handleCLILoginPoll))

	// Page-side API (callable by the trusted parent page JS).
	mux.HandleFunc("GET /api/uploads/{slug}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/uploads/{slug}/comments", s.handleAddComment)
	mux.HandleFunc("GET /api/uploads/{slug}/views", s.handleViews)

	// Pages & assets.
	mux.HandleFunc("GET /p/{slug}", s.handlePage)
	mux.HandleFunc("POST /p/{slug}", s.rateLimit(s.passwordLimiter, s.handlePagePassword))
	mux.HandleFunc("GET /raw/{slug}", s.handleRaw)
	mux.HandleFunc("GET /bridge.js", s.handleBridge)
	mux.HandleFunc("GET /app.js", s.handleApp)
	mux.HandleFunc("GET /dashboard-alpine.js", s.handleDashboardAlpine)
	mux.HandleFunc("GET /toast.js", s.handleToast)
	mux.HandleFunc("GET /alpine.min.js", s.handleAlpine)
	mux.HandleFunc("GET /pines.css", s.handlePines)
	mux.HandleFunc("GET /favicon.svg", s.handleFaviconSVG)
	mux.HandleFunc("GET /favicon.png", s.handleFaviconPNG)
	mux.HandleFunc("GET /favicon.ico", s.handleFaviconICO)
	mux.HandleFunc("GET /logo.svg", s.handleLogoSVG)
	mux.HandleFunc("GET /logo.png", s.handleLogoPNG)
	mux.HandleFunc("GET /", s.handleIndex)

	// Health checks (unauthenticated, for load balancers / orchestrators).
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Prometheus metrics (unauthenticated, for monitoring).
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	// Web GUI (browser-based management).
	mux.HandleFunc("GET /setup", s.handleSetup)
	mux.HandleFunc("POST /setup", s.rateLimit(s.loginLimiter, s.handleSetup))
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("POST /login", s.rateLimit(s.loginLimiter, s.handleLogin))
	mux.HandleFunc("GET /oauth/{provider}/start", s.rateLimit(s.loginLimiter, s.handleOAuthStart))
	mux.HandleFunc("GET /oauth/{provider}/callback", s.rateLimit(s.loginLimiter, s.handleOAuthCallback))
	mux.HandleFunc("GET /invite/{token}", s.handleInviteLink)
	mux.HandleFunc("GET /cli-login/{code}", s.handleCLILoginPage)
	mux.HandleFunc("POST /cli-login/{code}", s.handleCLILoginApprove)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /dashboard", s.handleDashboard)
	mux.HandleFunc("POST /dashboard/upload", s.handleDashboardUpload)
	mux.HandleFunc("POST /dashboard/delete/{slug}", s.handleDashboardDelete)
	mux.HandleFunc("POST /dashboard/settings", s.handleDashboardSettings)
	mux.HandleFunc("POST /dashboard/invites", s.handleDashboardCreateInvite)
	mux.HandleFunc("POST /dashboard/invites/revoke/{id}", s.handleDashboardRevokeInvite)
	mux.HandleFunc("POST /dashboard/users/{id}/admin", s.handleDashboardUserAdmin)
	mux.HandleFunc("POST /dashboard/users/{id}/disabled", s.handleDashboardUserDisabled)
	mux.HandleFunc("GET /dashboard/stats/{slug}", s.handleDashboardStats)

	return s.withMiddleware(mux)
}
