#!/bin/bash
# 检查 Master/Caves是否存活
pgrep dontstarve_dedicated_server_nullrenderer >/dev/null
if [ $? -eq 0 ]; then
  echo '{"healthy": true}'
else
  echo '{"healthy": false}'
fi