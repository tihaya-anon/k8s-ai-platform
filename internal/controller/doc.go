/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package controller holds one reconciler per kind in api/v1alpha1, plus the envtest suite they
// share. These are the platform's policy: Kueue owns queueing and quota and KServe owns serving
// and traffic, so what is left here is the lineage, the promotion gate, and the interface that
// survives swapping either of them out. A reconciler that only copies fields from one CRD into
// another does not belong in this package -- users can write the other resource themselves.
//
// Every reconciler here is level-triggered. It reads the world, makes it match the spec, and
// never assumes it saw the previous event, so reconciling twice has to equal reconciling once.
// Each one reports through status conditions rather than logs, emits an Event on every state
// transition, and sets an ownerReference on everything it creates. None of them writes fields on
// another operator's resources beyond the ones it owns: set yours, read their conditions, do not
// fight their controller.
//
// The package comment lives in its own file because the package grows by one reconciler per kind,
// and there is no single controller file it would still belong to afterwards.
package controller
