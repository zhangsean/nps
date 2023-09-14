#!/bin/sh
set -e

if [ -z "$1" ]; then
    [ ! -f /conf/nps.conf ] && cp -r /conf-tpl /conf
    /nps
else
    exec "$@"
fi
