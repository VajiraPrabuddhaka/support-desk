#!/usr/bin/env bash
# Builds the console's image and one image per skill, and pushes them to the
# cluster's registry. OpenChoreo builds the agents from GitHub (see
# scripts/deploy-agents.sh), but the console and the skills are still built here:
# the console runs from the image of the Dockerfile at the root, and an Agent
# takes a skill only as an OCI image for now. Each tag is a hash of its sources,
# so a change gets a new tag and a new release.
. "$(dirname "$0")/common.sh"

console_tag=$(content_hash agents console internal tools go.mod go.sum Dockerfile)
console_image="support-desk/console:$console_tag"
log "Console image $REGISTRY_PUSH/$console_image"
docker build -q -t "$REGISTRY_PUSH/$console_image" .
docker push -q "$REGISTRY_PUSH/$console_image"

{
  echo "# Written by openchoreo/scripts/push-images.sh"
  echo "CONSOLE_IMAGE=$REGISTRY_PULL/$console_image"
} > "$IMAGES_ENV"

# A skill image holds the skill's files at its root, as the Agent controller and
# the image volume that mounts it expect.
for dir in agents/*/skills/*/; do
  name=$(basename "$dir")
  skill_image="support-desk/skills/$name:$(content_hash "$dir")"
  log "Skill image $REGISTRY_PUSH/$skill_image"
  printf 'FROM scratch\nCOPY . /\n' | docker build -q -t "$REGISTRY_PUSH/$skill_image" -f - "$dir"
  docker push -q "$REGISTRY_PUSH/$skill_image"
  var=$(echo "$name" | tr 'a-z-' 'A-Z_')_SKILL
  echo "$var=$REGISTRY_PULL/$skill_image" >> "$IMAGES_ENV"
done

log "Wrote $IMAGES_ENV"
cat "$IMAGES_ENV"
