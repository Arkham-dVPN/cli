package arkham_protocol

import (
	"context"
	"fmt"

	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go/rpc"
)

// FetchAllWardens fetches all Warden accounts from the blockchain.
func (client *Client) FetchAllWardens() ([]*Warden, error) {
	var wardenAccounts []*Warden

	// Get all accounts owned by the program, filtered by the Warden discriminator.
	resp, err := client.RpcClient.GetProgramAccountsWithOpts(
		context.Background(),
		ProgramID,
		&rpc.GetProgramAccountsOpts{
			Filters: []rpc.RPCFilter{
				{
					Memcmp: &rpc.RPCFilterMemcmp{
						Offset: 0,
						Bytes:  Account_Warden[:],
					},
				},
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get program accounts: %w", err)
	}

	// Deserialize each account
	for _, account := range resp {
		var warden Warden
		err := warden.UnmarshalWithDecoder(bin.NewBorshDecoder(account.Account.Data.GetBinary()))
		if err != nil {
			// Log the error but continue with other accounts
			fmt.Printf("failed to deserialize warden account %s: %v\n", account.Pubkey.String(), err)
			continue
		}
		wardenAccounts = append(wardenAccounts, &warden)
	}

	return wardenAccounts, nil
}