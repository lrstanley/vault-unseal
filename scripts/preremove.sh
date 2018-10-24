#!/bin/bash

# 1 == upgrade
# 0 == uninstall
if [ "$1" == 0 ];then
  systemctl stop vault-unseal.service
  systemctl disable vault-unseal.service
fi
