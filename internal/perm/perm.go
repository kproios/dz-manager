// Package perm defines what each role may do and which permission every
// panel API route needs. Routes are deny-by-default: a route missing from
// Routes is refused for everyone (and a test fails the build).
package perm

import (
	"net/http"
	"slices"
	"strings"
)

// Permission is one thing a user may do.
type Permission string

// Permissions.
const (
	// Self is any signed-in user (their own account, preferences, the stream).
	Self Permission = "self"
	// ViewLive: dashboard, live players, map, chat, events, player database.
	ViewLive Permission = "view.live"
	// PlayersPrivate: reveal IP addresses and run Steam/IP checks.
	PlayersPrivate Permission = "players.private"
	// PlayersModerate: message and kick players.
	PlayersModerate Permission = "players.moderate"
	// BansManage: bans, ban lists, the review queue and the priority queue.
	BansManage Permission = "bans.manage"
	// ActionsSafe: teleport, heal, vehicle refuel/repair/flip/stop engine.
	ActionsSafe Permission = "actions.safe"
	// ActionsStrong: kill, strip, give items, delete vehicles.
	ActionsStrong Permission = "actions.strong"
	// ServerControl: lock/unlock, kick everyone, restart schedule and restarts.
	ServerControl Permission = "server.control"
	// SettingsManage: messages, Discord, map, tracking, intel, retention, bot info.
	SettingsManage Permission = "settings.manage"
	// ServersManage: server profiles, RCON credentials, ingest tokens, API keys.
	ServersManage Permission = "servers.manage"
	// UsersManage: users, roles, invites and the full audit log.
	UsersManage Permission = "users.manage"
)

// Info describes a permission for the UI.
type Info struct {
	Name  Permission `json:"name"`
	Label string     `json:"label"`
}

// All lists the permissions in UI order (Self is implied and not listed).
var All = []Info{
	{ViewLive, "Live view (players, map, chat, events, performance)"},
	{PlayersPrivate, "Private player data (IP addresses, Steam checks)"},
	{PlayersModerate, "Message and kick players"},
	{BansManage, "Bans, review queue and priority queue"},
	{ActionsSafe, "Safe actions (teleport, heal, vehicle repair, refuel, flip, stop)"},
	{ActionsStrong, "Strong actions (kill, strip, give items, delete vehicle)"},
	{ServerControl, "Lock, kick everyone, restarts"},
	{SettingsManage, "Settings (messages, Discord, map, intel, retention)"},
	{ServersManage, "Server profiles, RCON, ingest tokens, API keys"},
	{UsersManage, "Users, roles and the full audit log"},
}

// Role is a fixed set of permissions.
type Role string

// Roles, most powerful first.
const (
	Owner     Role = "owner"
	Admin     Role = "admin"
	Moderator Role = "moderator"
	Viewer    Role = "viewer"
)

// RoleInfo describes a role for the UI.
type RoleInfo struct {
	Name  Role         `json:"name"`
	Label string       `json:"label"`
	Help  string       `json:"help"`
	Perms []Permission `json:"permissions"`
}

// Roles lists the roles in UI order.
var Roles = []RoleInfo{
	{Owner, "Owner", "Everything, including users, server profiles and API keys. There is always at least one owner.",
		[]Permission{ViewLive, PlayersPrivate, PlayersModerate, BansManage, ActionsSafe, ActionsStrong, ServerControl, SettingsManage, ServersManage, UsersManage}},
	{Admin, "Admin", "Runs the server: every game action, restarts and settings. Cannot manage users, server profiles or API keys.",
		[]Permission{ViewLive, PlayersPrivate, PlayersModerate, BansManage, ActionsSafe, ActionsStrong, ServerControl, SettingsManage}},
	{Moderator, "Moderator", "Looks after players: messages, kicks, bans, priority and safe actions.",
		[]Permission{ViewLive, PlayersPrivate, PlayersModerate, BansManage, ActionsSafe}},
	{Viewer, "Viewer", "Read only: live view, map, chat, events and the player database (no IP addresses).",
		[]Permission{ViewLive}},
}

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool {
	return slices.ContainsFunc(Roles, func(x RoleInfo) bool { return string(x.Name) == r })
}

// Of returns the permissions of a role (nil for an unknown role).
func Of(r Role) []Permission {
	for _, x := range Roles {
		if x.Name == r {
			return x.Perms
		}
	}
	return nil
}

// Has reports whether role r holds permission p.
func Has(r Role, p Permission) bool {
	if p == Self {
		return ValidRole(string(r))
	}
	return slices.Contains(Of(r), p)
}

// Rank orders roles: a lower number is more powerful.
func Rank(r Role) int {
	for i, x := range Roles {
		if x.Name == r {
			return i
		}
	}
	return len(Roles)
}

// Rule is what a route needs.
type Rule struct {
	Perm Permission
	// StepUp routes need a 2FA confirmation from the last 10 minutes.
	StepUp bool
	// Dynamic, if set, picks the permission from the request (e.g. the action name).
	Dynamic func(r *http.Request) (Permission, bool)
}

// StrongActions are the game actions that need ActionsStrong.
var StrongActions = []string{"kill", "strip", "give_item", "vehicle_delete"}

func actionRule(r *http.Request) (Permission, bool) {
	name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if slices.Contains(StrongActions, name) {
		return ActionsStrong, true
	}
	return ActionsSafe, false
}

func revealIP(base Permission) func(r *http.Request) (Permission, bool) {
	return func(r *http.Request) (Permission, bool) {
		if r.URL.Query().Get("reveal_ip") == "1" {
			return PlayersPrivate, false
		}
		return base, false
	}
}

// Routes maps every panel API route pattern (as registered on the API mux)
// to its rule. The route test in this package fails when a registered route
// is missing here or an entry here is no longer registered.
var Routes = map[string]Rule{
	// Everyone signed in.
	"GET /api/me":           {Perm: Self},
	"GET /api/prefs":        {Perm: Self},
	"PUT /api/prefs":        {Perm: Self},
	"GET /api/stream":       {Perm: Self},
	"GET /api/audit":        {Perm: Self}, // filtered to the user's own entries without UsersManage
	"GET /api/audit/search": {Perm: Self}, // same
	"/api/":                 {Perm: Self}, // the 404 catch-all

	// Own account.
	"GET /api/account":                        {Perm: Self},
	"PUT /api/account/password":               {Perm: Self, StepUp: true},
	"POST /api/account/confirm":               {Perm: Self},
	"POST /api/account/recovery-codes":        {Perm: Self, StepUp: true},
	"POST /api/account/2fa/start":             {Perm: Self, StepUp: true},
	"POST /api/account/2fa/finish":            {Perm: Self, StepUp: true},
	"DELETE /api/account/2fa":                 {Perm: Self, StepUp: true},
	"DELETE /api/account/sessions/{id}":       {Perm: Self},
	"POST /api/account/sessions/sign-out-all": {Perm: Self},
	"DELETE /api/account/steam":               {Perm: Self, StepUp: true},
	"DELETE /api/account/oidc":                {Perm: Self, StepUp: true},

	// Live view.
	"GET /api/status":             {Perm: ViewLive},
	"GET /api/capabilities":       {Perm: ViewLive},
	"GET /api/actions":            {Perm: ViewLive},
	"GET /api/vehicles/actions":   {Perm: ViewLive},
	"GET /api/chat":               {Perm: ViewLive},
	"GET /api/map/events":         {Perm: ViewLive},
	"GET /api/map/event":          {Perm: ViewLive},
	"GET /api/map/world":          {Perm: ViewLive},
	"GET /api/map/state":          {Perm: ViewLive},
	"GET /api/perf":               {Perm: ViewLive},
	"GET /api/intel":              {Perm: ViewLive},
	"GET /api/players/{id}/intel": {Perm: ViewLive, Dynamic: revealIP(ViewLive)},
	"GET /api/profiles":           {Perm: ViewLive},
	"GET /api/players":            {Perm: ViewLive},
	"GET /api/players/{id}":       {Perm: ViewLive},
	"GET /api/players/{id}/flags": {Perm: ViewLive},
	"GET /api/server/status":      {Perm: ViewLive},
	"GET /api/server/players":     {Perm: ViewLive, Dynamic: revealIP(ViewLive)},
	"POST /api/server/refresh":    {Perm: ViewLive},
	"GET /api/events":             {Perm: ViewLive},
	"GET /api/bridge":             {Perm: ViewLive},
	"GET /api/restarts":           {Perm: ViewLive},
	"GET /api/bans":               {Perm: ViewLive},

	// Private player data.
	"POST /api/players/{id}/intel/refresh": {Perm: PlayersPrivate},

	// Message and kick.
	"POST /api/rcon/say":  {Perm: PlayersModerate},
	"POST /api/rcon/kick": {Perm: PlayersModerate},

	// Bans, review queue, priority.
	"POST /api/bans":                       {Perm: BansManage, StepUp: true},
	"POST /api/bans/{id}/revoke":           {Perm: BansManage, StepUp: true},
	"GET /api/bans/reconcile":              {Perm: BansManage},
	"GET /api/bans/export":                 {Perm: BansManage},
	"GET /api/banlists":                    {Perm: BansManage},
	"POST /api/banlists/import":            {Perm: BansManage, StepUp: true},
	"POST /api/players/{id}/review":        {Perm: BansManage},
	"GET /api/review-queue":                {Perm: BansManage},
	"GET /api/priority":                    {Perm: BansManage},
	"POST /api/priority":                   {Perm: BansManage},
	"POST /api/priority/{steam64}/remove":  {Perm: BansManage},
	"POST /api/priority/{steam64}/applied": {Perm: BansManage},
	"GET /api/priority/export":             {Perm: BansManage},

	// Game actions: the action name decides safe or strong.
	"POST /api/players/{id}/actions/{action}":  {Perm: ActionsSafe, Dynamic: actionRule},
	"POST /api/vehicles/{id}/actions/{action}": {Perm: ActionsSafe, Dynamic: actionRule},

	// Server control.
	"POST /api/rcon/kick-all":               {Perm: ServerControl, StepUp: true},
	"POST /api/rcon/lock":                   {Perm: ServerControl},
	"POST /api/rcon/unlock":                 {Perm: ServerControl},
	"PUT /api/restarts/schedule":            {Perm: ServerControl},
	"POST /api/restarts/drive-verification": {Perm: ServerControl},
	"POST /api/restarts/now":                {Perm: ServerControl, StepUp: true},
	"POST /api/restarts/cancel":             {Perm: ServerControl},
	"POST /api/restarts/skip":               {Perm: ServerControl},

	// Settings.
	"GET /api/settings/messages":            {Perm: SettingsManage},
	"PUT /api/settings/messages":            {Perm: SettingsManage},
	"POST /api/settings/messages/send":      {Perm: SettingsManage},
	"GET /api/settings/tracking":            {Perm: SettingsManage},
	"PUT /api/settings/tracking":            {Perm: SettingsManage},
	"GET /api/settings/map":                 {Perm: SettingsManage},
	"PUT /api/settings/map":                 {Perm: SettingsManage},
	"GET /api/settings/discord":             {Perm: SettingsManage},
	"PUT /api/settings/discord/{key}":       {Perm: SettingsManage, StepUp: true},
	"POST /api/settings/discord/{key}/test": {Perm: SettingsManage},
	"GET /api/settings/intel":               {Perm: SettingsManage},
	"PUT /api/settings/intel":               {Perm: SettingsManage, StepUp: true},
	"GET /api/settings/bot":                 {Perm: SettingsManage},
	"PUT /api/settings/bot/info":            {Perm: SettingsManage},
	"GET /api/settings/retention":           {Perm: SettingsManage},
	"PUT /api/settings/retention":           {Perm: SettingsManage},
	"POST /api/archives/run":                {Perm: SettingsManage},
	"GET /api/archives/{name}":              {Perm: SettingsManage},
	"PUT /api/map/world":                    {Perm: SettingsManage},
	"GET /api/map/sources":                  {Perm: SettingsManage},
	"GET /api/map/install":                  {Perm: SettingsManage},
	"POST /api/map/install":                 {Perm: SettingsManage},
	"GET /api/events/raw":                   {Perm: SettingsManage},

	// Server profiles and API keys.
	"POST /api/profiles":                    {Perm: ServersManage, StepUp: true},
	"PUT /api/profiles/{id}":                {Perm: ServersManage, StepUp: true},
	"POST /api/profiles/{id}/activate":      {Perm: ServersManage},
	"POST /api/profiles/{id}/archive":       {Perm: ServersManage, StepUp: true},
	"POST /api/profiles/{id}/rotate-ingest": {Perm: ServersManage, StepUp: true},
	"GET /api/profiles/{id}/wizard":         {Perm: ServersManage},
	"GET /api/settings/api-keys":            {Perm: ServersManage},
	"POST /api/settings/api-keys":           {Perm: ServersManage, StepUp: true},
	"DELETE /api/settings/api-keys/{id}":    {Perm: ServersManage, StepUp: true},

	// Users.
	"GET /api/users":                 {Perm: UsersManage},
	"POST /api/users/invites":        {Perm: UsersManage, StepUp: true},
	"DELETE /api/users/invites/{id}": {Perm: UsersManage},
	"PUT /api/users/{id}/role":       {Perm: UsersManage, StepUp: true},
	"POST /api/users/{id}/disable":   {Perm: UsersManage, StepUp: true},
	"POST /api/users/{id}/enable":    {Perm: UsersManage, StepUp: true},
	"POST /api/users/{id}/reset-2fa": {Perm: UsersManage, StepUp: true},
	"POST /api/users/{id}/sign-out":  {Perm: UsersManage},
	"DELETE /api/users/{id}":         {Perm: UsersManage, StepUp: true},
}

// Resolve returns the permission a request needs on a route pattern and
// whether it needs a recent 2FA confirmation. ok is false for unknown routes.
func Resolve(pattern string, r *http.Request) (p Permission, stepUp, ok bool) {
	rule, ok := Routes[pattern]
	if !ok {
		return "", false, false
	}
	p, stepUp = rule.Perm, rule.StepUp
	if rule.Dynamic != nil {
		dp, ds := rule.Dynamic(r)
		p, stepUp = dp, stepUp || ds
	}
	return p, stepUp, true
}

// Scope is an API key scope and the permission its owner needs to use it.
var ScopePerm = map[string]Permission{
	"public":        ViewLive,
	"players:read":  ViewLive,
	"bans:read":     BansManage,
	"priority:read": BansManage,
	"map:read":      ViewLive,
	"actions:write": ActionsSafe,
}
