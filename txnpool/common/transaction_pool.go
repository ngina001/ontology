/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

// Package common provides constants, common types for other packages
package common

import (
	"sort"
	"sync"

	"github.com/ontio/ontology/common"
	"github.com/ontio/ontology/common/config"
	"github.com/ontio/ontology/common/log"
	"github.com/ontio/ontology/core/types"
	"github.com/ontio/ontology/errors"
	vt "github.com/ontio/ontology/validator/types"
)

type TXAttr struct {
	Height  uint32         // The height in which tx was verified
	Type    vt.VerifyType  // The validator flag: stateless/stateful
	ErrCode errors.ErrCode // Verified result
}

type TXEntry struct {
	Tx    *types.Transaction // transaction which has been verified
	Attrs []*TXAttr          // the result from each validator
}

type VerifiedTx struct {
	Tx             *types.Transaction // transaction which has been verified
	VerifiedHeight uint32
}

func (self *VerifiedTx) GetAttrs() []*TXAttr {
	return []*TXAttr{
		{
			Height:  0,
			Type:    vt.Stateless,
			ErrCode: errors.ErrNoError,
		}, {
			Height:  self.VerifiedHeight,
			Type:    vt.Stateful,
			ErrCode: errors.ErrNoError,
		},
	}
}

// TXPool contains all currently valid transactions. Transactions
// enter the pool when they are valid from the network,
// consensus or submitted. They exit the pool when they are included
// in the ledger.
type TXPool struct {
	sync.RWMutex
	txList map[common.Uint256]*VerifiedTx // Transactions which have been verified
}

func NewTxPool() *TXPool {
	return &TXPool{
		txList: make(map[common.Uint256]*VerifiedTx),
	}
}

// AddTxList adds a valid transaction to the transaction pool. If the
// transaction is already in the pool, just return false. Parameter
// txEntry includes transaction, fee, and verified information(height,
// validator, error code).
func (tp *TXPool) AddTxList(txEntry *VerifiedTx) bool {
	tp.Lock()
	defer tp.Unlock()
	txHash := txEntry.Tx.Hash()
	if _, ok := tp.txList[txHash]; ok {
		log.Infof("AddTxList: transaction %x is already in the pool", txHash)
		return false
	}

	tp.txList[txHash] = txEntry
	return true
}

// CleanTransactionList cleans the transaction list included in the ledger.
func (tp *TXPool) CleanTransactionList(txs []*types.Transaction) {
	cleaned := 0
	txsNum := len(txs)
	tp.Lock()
	defer tp.Unlock()
	for _, tx := range txs {
		if _, ok := tp.txList[tx.Hash()]; ok {
			delete(tp.txList, tx.Hash())
			cleaned++
		}
	}

	log.Debugf("clean txes: total %d, cleaned %d, remains %d in TxPool", txsNum, cleaned, len(tp.txList))
}

// DelTxList removes a single transaction from the pool.
func (tp *TXPool) DelTxList(tx *types.Transaction) bool {
	tp.Lock()
	defer tp.Unlock()
	txHash := tx.Hash()
	if _, ok := tp.txList[txHash]; !ok {
		return false
	}
	delete(tp.txList, txHash)
	return true
}

// isVerfiyExpired compares a verifed transaction's height with the next
// block height from consensus. If the height is less than the next block
// height, re-verify it.
func (tp *TXPool) isVerfiyExpired(txEntry *VerifiedTx, height uint32) bool {
	return txEntry.VerifiedHeight < height
}

// GetTxPool gets the transaction lists from the pool for the consensus,
// if the byCount is marked, return the configured number at most; if the
// the byCount is not marked, return all of the current transaction pool.
func (tp *TXPool) GetTxPool(byCount bool, height uint32) ([]*VerifiedTx, []*types.Transaction) {
	tp.RLock()
	defer tp.RUnlock()

	orderByFee := make([]*VerifiedTx, 0, len(tp.txList))
	for _, txEntry := range tp.txList {
		orderByFee = append(orderByFee, txEntry)
	}
	sort.Sort(OrderByNetWorkFee(orderByFee))

	count := int(config.DefConfig.Consensus.MaxTxInBlock)
	if count <= 0 {
		byCount = false
	}
	if len(tp.txList) < count || !byCount {
		count = len(tp.txList)
	}

	txList := make([]*VerifiedTx, 0, count)
	oldTxList := make([]*types.Transaction, 0)
	for _, txEntry := range orderByFee {
		if tp.isVerfiyExpired(txEntry, height) {
			oldTxList = append(oldTxList, txEntry.Tx)
			continue
		}
		txList = append(txList, txEntry)
		if len(txList) >= count {
			break
		}
	}

	return txList, oldTxList
}

// GetTransaction returns a transaction if it is contained in the pool
// and nil otherwise.
func (tp *TXPool) GetTransaction(hash common.Uint256) *types.Transaction {
	tp.RLock()
	defer tp.RUnlock()
	if tx := tp.txList[hash]; tx == nil {
		return nil
	}
	return tp.txList[hash].Tx
}

// GetTxStatus returns a transaction status if it is contained in the pool
// and nil otherwise.
func (tp *TXPool) GetTxStatus(hash common.Uint256) *TxStatus {
	tp.RLock()
	defer tp.RUnlock()
	txEntry, ok := tp.txList[hash]
	if !ok {
		return nil
	}
	ret := &TxStatus{
		Hash:  hash,
		Attrs: txEntry.GetAttrs(),
	}
	return ret
}

// GetTransactionCount returns the tx number of the pool.
func (tp *TXPool) GetTransactionCount() int {
	tp.RLock()
	defer tp.RUnlock()
	return len(tp.txList)
}

// GetTransactionCount returns the tx number of the pool.
func (tp *TXPool) GetTransactionHashList() []common.Uint256 {
	tp.RLock()
	defer tp.RUnlock()
	ret := make([]common.Uint256, 0, len(tp.txList))
	for txHash := range tp.txList {
		ret = append(ret, txHash)
	}
	return ret
}

// GetUnverifiedTxs checks the tx list in the block from consensus,
// and returns verified tx list, unverified tx list, and
// the tx list to be re-verified
func (tp *TXPool) GetUnverifiedTxs(txs []*types.Transaction, height uint32) *CheckBlkResult {
	tp.Lock()
	defer tp.Unlock()
	res := &CheckBlkResult{
		VerifiedTxs:   make([]*VerifyTxResult, 0, len(txs)),
		UnverifiedTxs: make([]*types.Transaction, 0),
		OldTxs:        make([]*types.Transaction, 0),
	}
	for _, tx := range txs {
		txEntry := tp.txList[tx.Hash()]
		if txEntry == nil {
			res.UnverifiedTxs = append(res.UnverifiedTxs, tx)
			continue
		}

		if !tp.isVerfiyExpired(txEntry, height) {
			delete(tp.txList, tx.Hash())
			res.OldTxs = append(res.OldTxs, txEntry.Tx)
			continue
		}

		res.VerifiedTxs = append(res.VerifiedTxs, &VerifyTxResult{
			Height:  txEntry.VerifiedHeight,
			Tx:      txEntry.Tx,
			ErrCode: errors.ErrNoError,
		})
	}

	return res
}

// RemoveTxsBelowGasPrice drops all transactions below the gas price
func (tp *TXPool) RemoveTxsBelowGasPrice(gasPrice uint64) {
	tp.Lock()
	defer tp.Unlock()
	for _, txEntry := range tp.txList {
		if txEntry.Tx.GasPrice < gasPrice {
			delete(tp.txList, txEntry.Tx.Hash())
		}
	}
}

// Remain returns the remaining tx list to cleanup
func (tp *TXPool) Remain() []*types.Transaction {
	tp.Lock()
	defer tp.Unlock()

	txList := make([]*types.Transaction, 0, len(tp.txList))
	for _, txEntry := range tp.txList {
		txList = append(txList, txEntry.Tx)
		delete(tp.txList, txEntry.Tx.Hash())
	}

	return txList
}
