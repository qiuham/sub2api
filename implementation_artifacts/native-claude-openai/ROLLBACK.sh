#!/usr/bin/env bash
set -euo pipefail
repo="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$repo"
for path in \
  backend/internal/service/gateway_scheduling.go \
  backend/internal/service/openai_messages_dispatch.go \
  backend/internal/service/openai_messages_dispatch_test.go \
  frontend/src/views/admin/GroupsView.vue \
  frontend/src/views/admin/groupsMessagesDispatch.ts \
  frontend/src/views/admin/__tests__/groupsMessagesDispatch.spec.ts; do
  git show "HEAD:$path" > "$path"
done
echo "restored source paths from HEAD"
