package bridge

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/bacalhau-project/lilypad/hardhat/artifacts/contracts/LilypadEvents.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog/log"
)

type SmartContract interface {
	Listen(context.Context, chan<- ContractSubmittedEvent) error

	Complete(context.Context, BacalhauJobCompletedEvent) (ContractPaidEvent, error)

	Refund(context.Context, ContractFailedEvent) (ContractRefundedEvent, error)
}

type realContract struct {
	client     *ethclient.Client
	contract   *LilypadEvents.LilypadEvents
	privateKey *ecdsa.PrivateKey

	maxSeenBlock uint64
}

func (r *realContract) publicKey() *ecdsa.PublicKey {
	return r.privateKey.Public().(*ecdsa.PublicKey)
}

func (r *realContract) wallet() common.Address {
	return crypto.PubkeyToAddress(*r.publicKey())
}

func (r *realContract) pendingNonce(ctx context.Context) (uint64, error) {
	return r.client.PendingNonceAt(ctx, r.wallet())
}

func (r *realContract) prepareTransaction(ctx context.Context) (*bind.TransactOpts, error) {
	nonce, err := r.pendingNonce(ctx)
	if err != nil {
		return nil, err
	}

	opts, err := bind.NewKeyedTransactorWithChainID(r.privateKey, big.NewInt(3141))
	if err != nil {
		return nil, err
	}

	opts.Nonce = big.NewInt(int64(nonce))
	opts.Value = big.NewInt(0)
	opts.Context = ctx

	return opts, nil
}

// Complete implements SmartContract
func (r *realContract) Complete(ctx context.Context, event BacalhauJobCompletedEvent) (ContractPaidEvent, error) {
	opts, err := r.prepareTransaction(ctx)
	if err != nil {
		return nil, err
	}

	var result string
	switch event.OrderResultType() {
	case ResultTypeCID:
		result = event.Result().String()
	case ResultTypeStdOut:
		result = event.StdOut()
	case ResultTypeStdErr:
		result = event.StdErr()
	case ResultTypeExitCode:
		result = fmt.Sprint(event.ExitCode())
	}

	txn, err := r.contract.LilypadEventsTransactor.ReturnBacalhauResults(
		opts,
		event.OrderRequestor(),
		big.NewInt(event.OrderNumber()),
		uint8(event.OrderResultType()),
		result,
	)
	if err != nil {
		return nil, err
	}

	log.Ctx(ctx).Info().Stringer("txn", txn.Hash()).Msg("Results returned")
	return event.Paid(), nil
}

// Refund implements SmartContract
func (r *realContract) Refund(ctx context.Context, event ContractFailedEvent) (ContractRefundedEvent, error) {
	opts, err := r.prepareTransaction(ctx)
	if err != nil {
		return nil, err
	}

	txn, err := r.contract.LilypadEventsTransactor.ReturnBacalhauError(
		opts,
		event.OrderRequestor(),
		big.NewInt(event.OrderNumber()),
		event.Error(),
	)
	if err != nil {
		return nil, err
	}

	log.Ctx(ctx).Info().Stringer("txn", txn.Hash()).Msg("Error returned")
	return event.Refunded(), nil
}

// Listen implements SmartContract
func (r *realContract) Listen(ctx context.Context, out chan<- ContractSubmittedEvent) error {
	scheduler := gocron.NewScheduler(time.UTC)
	_, err := scheduler.Every(15*time.Second).SingletonMode().Do(r.ReadLogs, ctx, out)
	if err != nil {
		return err
	}

	scheduler.StartAsync()
	defer scheduler.Stop()

	<-ctx.Done()
	return nil
}

func (r *realContract) ReadLogs(ctx context.Context, out chan<- ContractSubmittedEvent) {
	log.Ctx(ctx).Debug().Uint64("fromBlock", r.maxSeenBlock+1).Msg("Polling for smart contract events")

	// We deliberately ask for the current block *before* we make the events
	// call. It's possible that a block will be written between the two calls:
	//
	//    FilterNewJobs(block: #1) -> seen block #1
	//    block #2 gets written
	//    BlockNumber() -> block #3
	//    ...
	//    FilterNewJobs(block: #3)
	//
	// In this case we would never see any events in block #2. So we instead
	// remember the block number before the events call, and if a block is
	// written between them, we will get it again next time we ask for events.
	currentBlock, err := r.client.BlockNumber(ctx)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Send()
	}

	opts := bind.FilterOpts{Start: uint64(r.maxSeenBlock + 1), Context: ctx}
	logs, err := r.contract.LilypadEventsFilterer.FilterNewBacalhauJobSubmitted(&opts)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Send()
		// if we have an error here we should exit and restart
		// this avoids problems if the network is down and we are holding onto a previous
		// block number that is later than 24 hours and we get into a death loop
		// if the network is down or for whatever other reason we are unable
		// to list the events we should exit and restart to try and start again
		os.Exit(1)
	}
	defer logs.Close()

	r.maxSeenBlock = currentBlock

	for logs.Next() {
		recvEvent := logs.Event
		log.Ctx(ctx).Debug().
			Stringer("txn", recvEvent.Raw.TxHash).
			Uint64("block#", recvEvent.Raw.BlockNumber).
			Str("spec", recvEvent.Job.Spec).
			Bool("removed", recvEvent.Raw.Removed).
			Msg("Event")

		if recvEvent.Raw.Removed {
			continue
		}

		if !ResultType(recvEvent.Job.ResultType).Valid() {
			log.Ctx(ctx).Warn().Uint8("resultType", recvEvent.Job.ResultType).Msg("invalid ResultType")
			continue
		}

		out <- &event{
			orderId:         recvEvent.Raw.TxHash.Bytes(),
			orderOwner:      recvEvent.Job.Requestor.Bytes(),
			orderNumber:     recvEvent.Job.Id.Int64(),
			orderResultType: recvEvent.Job.ResultType,
			state:           OrderStateSubmitted,
			jobSpec:         []byte(recvEvent.Job.Spec),
		}

		r.maxSeenBlock = recvEvent.Raw.BlockNumber
	}
}

func NewContract(contractAddr common.Address, privateKey *ecdsa.PrivateKey) (SmartContract, error) {
	rpcEndpoint := "wss://wss.hyperspace.node.glif.io/apigw/lotus/rpc/v1"

	log.Debug().Str("endpoint", rpcEndpoint).Msg("Dial")
	client, err := ethclient.Dial(rpcEndpoint)
	if err != nil {
		return nil, err
	}

	contract, err := LilypadEvents.NewLilypadEvents(contractAddr, client)
	if err != nil {
		return nil, err
	}

	number, err := client.BlockNumber(context.Background())
	if err != nil {
		return nil, err
	}

	return &realContract{client, contract, privateKey, number}, nil
}
