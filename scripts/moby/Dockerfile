FROM sveil/os

# replace this with `sveil/os-initrd`
RUN mkdir /tmp/initrd \
  && cd /tmp/initrd \
  && cat /dist/initrd-* | gunzip | cpio -i \
  && rm -rf usr/lib \
  && rm /tmp/initrd/usr/var/lib/cni/bin/host-local /tmp/initrd/usr/var/lib/cni/bin/bridge \
  && mkdir -p /tmp/initrd/var/lib/cni/bin \
  && ln -s ../../../../usr/bin/ros /tmp/initrd/var/lib/cni/bin/host-local \
  && ln -s ../../../../usr/bin/ros /tmp/initrd/var/lib/cni/bin/bridge \
  && cp -r --update --dereference --force /tmp/initrd/* / \
  && cd / \
  && rm -rf /tmp/initrd

#FROM sveil/os-installer
#RUN cp /bin/ros /init

#FROM sveil/os-installer
#RUN cp /bin/ros /init
