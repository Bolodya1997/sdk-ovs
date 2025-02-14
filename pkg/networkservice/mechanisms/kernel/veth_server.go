// Copyright (c) 2021 Nordix Foundation.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build linux

package kernel

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/next"
	"github.com/networkservicemesh/sdk/pkg/networkservice/utils/metadata"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/postpone"
	"github.com/pkg/errors"

	"github.com/networkservicemesh/sdk-ovs/pkg/tools/ifnames"
)

type kernelVethServer struct {
	bridgeName string
}

// NewVethServer - return a new Veth Server chain element for kernel mechanism
func NewVethServer(bridgeName string) networkservice.NetworkServiceServer {
	return &kernelVethServer{bridgeName}
}

// NewClient create a kernel veth server chain element which would be useful to do network plumbing
// for service client container
func (k *kernelVethServer) Request(ctx context.Context, request *networkservice.NetworkServiceRequest) (*networkservice.Connection, error) {
	logger := log.FromContext(ctx).WithField("kernelVethServer", "Request")

	_, isEstablished := ifnames.Load(ctx, metadata.IsClient(k))
	if !isEstablished {
		if err := setupVeth(ctx, logger, request.GetConnection(), k.bridgeName, metadata.IsClient(k)); err != nil {
			_ = resetVeth(ctx, logger, request.GetConnection(), k.bridgeName, metadata.IsClient(k))
			return nil, err
		}
	}
	postponeCtxFunc := postpone.ContextWithValues(ctx)

	conn, err := next.Server(ctx).Request(ctx, request)
	if err != nil && !isEstablished {
		closeCtx, cancelClose := postponeCtxFunc()
		defer cancelClose()
		if _, exists := ifnames.LoadAndDelete(closeCtx, metadata.IsClient(k)); exists {
			if kernelServerErr := resetVeth(closeCtx, logger, request.GetConnection(), k.bridgeName, metadata.IsClient(k)); kernelServerErr != nil {
				err = errors.Wrapf(err, "connection closed with error: %s", kernelServerErr.Error())
			}
		}
		return nil, err
	}

	return conn, err
}

func (k *kernelVethServer) Close(ctx context.Context, conn *networkservice.Connection) (*empty.Empty, error) {
	logger := log.FromContext(ctx).WithField("kernelVethServer", "Close")
	_, err := next.Server(ctx).Close(ctx, conn)

	if mechanism := kernel.ToMechanism(conn.GetMechanism()); mechanism != nil {
		var kernelServerErr error
		_, exists := ifnames.LoadAndDelete(ctx, metadata.IsClient(k))
		if exists {
			kernelServerErr = resetVeth(ctx, logger, conn, k.bridgeName, metadata.IsClient(k))
		}
		if err != nil && kernelServerErr != nil {
			return nil, errors.Wrap(err, kernelServerErr.Error())
		}
		if kernelServerErr != nil {
			return nil, kernelServerErr
		}
	}

	return &empty.Empty{}, err
}
