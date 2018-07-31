/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// package smr defines central registry for single method registration(smr).
package smr

import (
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

type methodRegistry struct {
	mu       sync.Mutex
	registry map[string]func(svr *grpc.Server, handler interface{})
}

var centralMethodRegistry = methodRegistry{registry: make(map[string]func(svr *grpc.Server, handler interface{}))}

func RegisterMethodGenerator(fullpath string, generator func(svr *grpc.Server, handler interface{})) {
	centralMethodRegistry.mu.Lock()
	centralMethodRegistry.registry[fullpath] = generator
	centralMethodRegistry.mu.Unlock()
}

func RegisterMethodOnServer(svr *grpc.Server, fullpath string, handler interface{}) {
	centralMethodRegistry.mu.Lock()
	gen, ok := centralMethodRegistry.registry[fullpath]
	centralMethodRegistry.mu.Unlock()
	if !ok {
		grpclog.Fatalf("smr: RegisterMethodOnServer cannot find %q in the known methods", fullpath)
	}
	gen(svr, handler)
}
