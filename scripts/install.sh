#!/usr/bin/env sh
set -e

version_name=${INFRACOST_VERSION:-latest}

os=$(uname | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | tr '[:upper:]' '[:lower:]' | sed -e s/x86_64/amd64/)

version=$version_name
url="https://github.com/infracost/infracost/releases/latest/download/infracost-$os-$arch.tar.gz"
if [ "$version_name" != "latest" ]; then
  # TODO: add pagination support
  resp=$(curl -sL https://api.github.com/repos/infracost/infracost/releases\?per_page\=100)
  versions=$(echo "$resp" | sed -n 's/.*\"tag_name\": \"\(.*\)\".*/\1/p')

  # If v0.x.y was passed in find the exact version
  version=$(echo "$versions" | grep "$version_name$" | head -n 1)

  # If v0.x was passed in find the latest v0.x version
  if [ -z "$version" ]; then
    version=$(echo "$versions" | grep "$version_name." | head -n 1)
  fi

  url="https://github.com/infracost/infracost/releases/download/$version/infracost-$os-$arch.tar.gz"
fi

echo "Downloading $version release of infracost-$os-$arch..."
curl -sL $url | tar xz -C /tmp

echo "Moving /tmp/infracost-$os-$arch to /usr/local/bin/infracost"

mv -f /tmp/infracost-$os-$arch /usr/local/bin/infracost

echo "Completed installing $(infracost --version)"
