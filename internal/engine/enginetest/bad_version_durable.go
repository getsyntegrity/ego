// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package enginetest

import (
	"context"
	"encoding/json"

	"google.golang.org/protobuf/proto"

	testpb "github.com/getsyntegrity/urd/internal/testpb"
	behaviorport "github.com/getsyntegrity/urd/port/behavior"
)

// BadVersionDurableStateBehavior returns an invalid version increment from HandleCommand
type BadVersionDurableStateBehavior struct {
	id string
}

// NewBadVersionDurableStateBehavior returns a behavior for the entity id.
func NewBadVersionDurableStateBehavior(id string) *BadVersionDurableStateBehavior {
	return &BadVersionDurableStateBehavior{id: id}
}

var _ behaviorport.DurableState = (*BadVersionDurableStateBehavior)(nil)

func (x *BadVersionDurableStateBehavior) ID() string {
	return x.id
}

func (x *BadVersionDurableStateBehavior) InitialState() proto.Message {
	return new(testpb.Account)
}

func (x *BadVersionDurableStateBehavior) HandleCommand(_ context.Context, _ proto.Message, priorVersion uint64, _ proto.Message) (proto.Message, uint64, error) {
	return &testpb.Account{
		AccountId:      x.id,
		AccountBalance: 500.00,
	}, priorVersion + 5, nil // +5 instead of +1
}

func (x *BadVersionDurableStateBehavior) MarshalBinary() ([]byte, error) {
	return json.Marshal(struct {
		ID string `json:"id"`
	}{ID: x.id})
}

func (x *BadVersionDurableStateBehavior) UnmarshalBinary(data []byte) error {
	aux := struct {
		ID string `json:"id"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	x.id = aux.ID
	return nil
}
