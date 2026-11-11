// Package views is the contract of the screens: one struct per screen, with
// snake_case json tags like domain and plan. The TUI imports only this package.
//
// The types belong to the screen and do not embed domain: F5.2 maps
// domain -> view. In F5.1 there are only types and Decode (no db); the demo
// data lives in testdata/views/*.json and must decode strictly.
package views
