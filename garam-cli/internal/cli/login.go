package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/lib/platform-sdk-go"
)

// newLoginCommand signs the CLI in by the device grant (ADR-0120).
func newLoginCommand() *cobra.Command {
	flags := &userFlags{}
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign the CLI in, approving it in a browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return c.login(ctx, cmd, !noBrowser)
		},
	}
	flags.bind(cmd)
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL to open rather than opening it")
	return cmd
}

func (c *userCommand) login(ctx context.Context, cmd *cobra.Command, browse bool) error {
	code, err := c.session.Start(ctx)
	if err != nil {
		return err
	}
	page := c.session.VerificationURL(code)
	prompt := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(prompt, "Your code is %s. Approve it at\n\n  %s\n\n", code.UserCode, page)
	if browse && openBrowser(page) == nil {
		_, _ = fmt.Fprintln(prompt, "Opened that page in your browser; waiting for the approval.")
	} else {
		_, _ = fmt.Fprintln(prompt, "Open that page in a browser; waiting for the approval.")
	}
	if err := c.session.Wait(ctx, code); err != nil {
		return err
	}
	who, err := c.me(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(prompt, "Signed in to %s as %s (%s).\n", c.session.Endpoint, who.Nickname, who.Grn)
	return nil
}

// openBrowser asks the desktop to open page, and reports whether it
// could ask; whether a browser came up is not something it can see.
func openBrowser(page string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, page)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// newLogoutCommand revokes the CLI's session and forgets its tokens.
func newLogoutCommand() *cobra.Command {
	flags := &userFlags{}
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the CLI's session and forget its tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			return c.logout(cmd)
		},
	}
	flags.bind(cmd)
	return cmd
}

func (c *userCommand) logout(cmd *cobra.Command) error {
	signedIn, err := c.session.Logout(cmd.Context())
	if err != nil {
		return err
	}
	if !signedIn {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Not signed in to %s.\n", c.session.Endpoint)
		return nil
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Signed out of %s.\n", c.session.Endpoint)
	return nil
}

// newWhoamiCommand names the signed-in user.
func newWhoamiCommand() *cobra.Command {
	flags := &userFlags{}
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show who the CLI is signed in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			who, err := c.me(cmd.Context())
			if err != nil {
				return err
			}
			return c.render(who, []string{"NICKNAME", "GRN", "ENDPOINT"}, func() [][]string {
				return [][]string{{who.Nickname, who.Grn, c.session.Endpoint}}
			})
		},
	}
	flags.bind(cmd)
	return cmd
}

func (c *userCommand) me(ctx context.Context) (*apigen.User, error) {
	client, err := c.session.SignedIn(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.MeWithResponse(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "me", errorx.KindUnavailable)
	}
	if resp.JSON200 == nil {
		return nil, apiFailure(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}
