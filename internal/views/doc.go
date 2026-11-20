// Package views is the contract of the screens: one struct per screen, with
// snake_case json tags like domain and plan. The TUI imports only this package.
//
// The types belong to the screen and do not embed domain. From F5.2b on the package also holds
// pure builders (Build*) that map domain, memory and report data to the screen types; they read
// no db, file or clock (the caller passes the data and Now). The demo data lives in
// testdata/views/*.json and must decode strictly.
package views
