#!/usr/bin/env bash
# Writes the support desk's secrets from .env to OpenBao, under secret/support-desk/.
# The SecretReferences in openchoreo/app/secret-references.yaml point there.
# The triage bot's Gitea token is written later, by scripts/bootstrap.sh.
. "$(dirname "$0")/common.sh"

log "OpenBao: secret/support-desk/openrouter"
json_from_env apiKey=OPENROUTER_API_KEY | openbao_put support-desk/openrouter

log "OpenBao: secret/support-desk/lago"
json_from_env \
  secretKeyBase=LAGO_SECRET_KEY_BASE \
  rsaPrivateKey=LAGO_RSA_PRIVATE_KEY \
  encryptionPrimaryKey=LAGO_ENCRYPTION_PRIMARY_KEY \
  encryptionDeterministicKey=LAGO_ENCRYPTION_DETERMINISTIC_KEY \
  encryptionKeyDerivationSalt=LAGO_ENCRYPTION_KEY_DERIVATION_SALT \
  adminPassword=LAGO_ADMIN_PASSWORD \
  apiKey=LAGO_API_KEY |
  openbao_put support-desk/lago
