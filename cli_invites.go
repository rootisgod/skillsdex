package skillsdex

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
)

func runInvites(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "create":
			opts, err := parseInviteCreateArgs(args[1:])
			if err != nil {
				return errors.New("usage: skillsdex invites create [--email EMAIL] [--json]")
			}
			client, err := authenticatedClient()
			if err != nil {
				return err
			}
			var result SignupInviteCreateResult
			body := SignupInviteCreateRequest{Email: opts.Email}
			if err := client.postJSON("/v1/invites", body, &result); err != nil {
				return err
			}
			if opts.JSON {
				return printJSON(result)
			}
			fmt.Printf("Created invite %s, expires %s.\n", result.Invite.ID, result.Invite.ExpiresAt.Format("2006-01-02 15:04"))
			fmt.Printf("Signup URL: %s\n", result.URL)
			if result.EmailTo != "" {
				if result.EmailID != "" {
					fmt.Printf("Email sent to %s.\n", result.EmailTo)
				} else if result.EmailError != "" {
					fmt.Fprintf(os.Stderr, "Email not sent: %s\n", result.EmailError)
				}
			}
			return nil
		case "revoke":
			if len(args) != 2 {
				return errors.New("usage: skillsdex invites revoke <invite-id>")
			}
			client, err := authenticatedClient()
			if err != nil {
				return err
			}
			var resp struct {
				Invite SignupInvite `json:"invite"`
			}
			if err := client.postJSON("/v1/invites/"+url.PathEscape(strings.TrimSpace(args[1]))+"/revoke", map[string]bool{}, &resp); err != nil {
				return err
			}
			fmt.Printf("Revoked invite %s.\n", resp.Invite.ID)
			return nil
		}
	}
	opts, err := parseInvitesArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex invites [--json] | skillsdex invites create [--email EMAIL] [--json] | skillsdex invites revoke <invite-id>")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp invitesResponse
	if err := client.getJSON("/v1/invites", &resp); err != nil {
		return err
	}
	if opts.JSON {
		return printJSON(resp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTOKEN\tSTATUS\tCREATED\tEXPIRES\tUSED_BY")
	for _, invite := range resp.Invites {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", invite.ID, invite.TokenPreview, signupInviteStatus(invite), invite.CreatedAt.Format("2006-01-02 15:04"), invite.ExpiresAt.Format("2006-01-02 15:04"), valueOrDash(invite.UsedBy))
	}
	return tw.Flush()
}
