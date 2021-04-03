#!/bin/bash

apt-get update
apt-get -y install build-essential asciidoc binutils bzip2 gawk gettext git libncurses5-dev \
  libz-dev patch python3 python2.7 unzip zlib1g-dev lib32gcc1 libc6-dev-i386 subversion \
  flex uglifyjs git-core gcc-multilib p7zip p7zip-full msmtp libssl-dev texinfo \
  libglib2.0-dev xmlto qemu-utils upx libelf-dev autoconf automake libtool autopoint \
  device-tree-compiler g++-multilib antlr3 gperf wget curl swig rsync apt-transport-https \
  ca-certificates gnupg lsb-release golang-go libncursesw5-dev
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg
echo \
  "deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian \
  $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
apt-get update
apt-get -y install docker-ce docker-ce-cli containerd.io
BIN_SH="$(ls -la /bin/sh)"
echo "${BIN_SH}"
if [ "${BIN_SH}" = *"bash"* ]; then
        echo "we already use bash"
else
        echo "we want to change dash to bash"
        echo "dash dash/sh boolean false" | debconf-set-selections
        sudo dpkg-reconfigure -f noninteractive dash
fi
go get github.com/github-release/github-release