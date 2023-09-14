#!/bin/sh
set -e

if [ "$1" == "sh" ]; then
    exec "$@"
else
    [ ! -f /conf/nps.conf ] && cp /conf-tpl/* /conf/
    /nps $@
fi