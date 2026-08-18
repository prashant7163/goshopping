// Package migrations embeds the SQL schema migrations so the server and the
// standalone migrate command carry them in the binary. Reading them from disk
// would make the working directory part of the deployment contract.
package migrations

import "embed"

// FS holds the numbered .up.sql/.down.sql pairs, in golang-migrate's layout.
//
//go:embed *.sql
var FS embed.FS
