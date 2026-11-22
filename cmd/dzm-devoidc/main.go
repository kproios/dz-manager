// Command dzm-devoidc runs a fake OpenID Connect issuer for local
// development and smoke tests. It is never part of a release.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/kproios/dz-manager/internal/auth/oidctest"
)

func main() {
	group := flag.String("group", "dzm-admins", "group claim for the fake user")
	flag.Parse()
	is := oidctest.New()
	is.User.Groups = []string{*group}
	fmt.Printf("DZM_OIDC_ISSUER=%s\nDZM_OIDC_CLIENT_ID=%s\nDZM_OIDC_CLIENT_SECRET=%s\n", is.URL(), is.ClientID, is.Secret)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	is.Close()
}
