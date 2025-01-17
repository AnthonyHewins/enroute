#!/bin/bash -e
#
# Copyright © 2019 VMware
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#VERSION=$(go list -m all | grep k8s.io/code-generator | rev | cut -d"-" -f1 | cut -d" " -f1 | rev)
#TMP_DIR=$(mktemp -d)
#git clone https://github.com/kubernetes/code-generator.git ${TMP_DIR}
#(cd ${TMP_DIR} && git reset --hard ${VERSION})

git clone https://github.com/kubernetes/code-generator.git
cd code-generator
git reset --hard remotes/origin/release-1.23
go install ./...
cd ..

GOPATH=${HOME}/go code-generator/generate-groups.sh \
  all \
  github.com/saarasio/enroute/enroute-dp/apis/generated \
  github.com/saarasio/enroute/enroute-dp/apis \
  "enroute:v1" \
  --output-base . \
  --go-header-file hack/boilerplate.go.tmpl \
  $@

rm -rf code-generator

# Copy the generated.deepcopy to the api packages
rm -rf apis/generated
cp -R github.com/saarasio/enroute/enroute-dp/apis/generated apis/
mv github.com/saarasio/enroute/enroute-dp/apis/enroute/v1/zz_generated.deepcopy.go apis/enroute/v1
rm -rf github.com
