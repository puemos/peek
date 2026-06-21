package cli

import (
	"fmt"
	"os"

	"github.com/puemos/peek/internal/version"
)

// Run dispatches a command. args is argv without the program name.
func Run(args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "login":
		err = cmdLogin(rest)
	case "config":
		err = cmdConfig(rest)
	case "upload":
		err = cmdUpload(rest)
	case "list":
		err = cmdList(rest)
	case "delete":
		err = cmdDelete(rest)
	case "password":
		err = cmdPassword(rest)
	case "stats":
		err = cmdStats(rest)
	case "comments":
		err = cmdComments(rest)
	case "export":
		err = cmdExport(rest)
	case "delete-all":
		err = cmdDeleteAll(rest)
	case "token":
		err = cmdToken(rest)
	case "help", "-h", "--help":
		usage()
		return 0
	case "version", "-v", "--version":
		fmt.Println("peek " + version.String())
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `peek — Peek CLI

Usage:
  peek login [--host <url>]             sign in with browser login when available
  peek login --token-stdin              read an access token from stdin
  peek config set --host <url>          set host (use 'login' / --token-stdin for the token)
  peek config show
  peek upload <file.html> [--password <pw>] [--name <filename>]
  peek upload <file.html> --password-stdin
  peek list
  peek delete <slug>
  peek password <slug> --set <pw>      protect a page
  peek password <slug> --set-stdin     protect a page, reading password from stdin
  peek password <slug> --clear         remove protection
  peek stats <slug>
  peek comments <slug>                 list comments on one of your uploads
  peek export <slug>                   export all data for an upload
  peek delete-all                      delete all your uploads
  peek token create --name <name>      create a new user token (admin only)
  peek token list                      list tokens (admin only)
  peek token revoke <id>               revoke a token by id (admin only)

Token input (most secure first):
  peek login                           browser login when available; otherwise hidden prompt
  peek login --token-stdin             read token from a pipe
  peek login --token-file <path>       read token from a file
  peek login --token <token>           discouraged: exposed in history & 'ps'
  PEEK_TOKEN=…  (env override)          handy for CI
  peek config set --token-stdin        read token from a pipe
  peek config set --token-file <path>  read token from a file
  peek config set --token <token>      discouraged: exposed in history & 'ps'

Environment overrides:
  PEEK_HOST, PEEK_TOKEN
`)
}
