#!/bin/sh
set -e

if [ "$1" == "sh" ]; then
    exec "$@"
else
    [ ! -f /conf/nps.conf ] && cp -r /conf-tpl /conf
    /nps $@
fi
