# Commandes proxy

Ce plugin porte dans Gate les fonctions compatibles de GateProxy et
CustomF3Brand : `/kick` (console uniquement) et l’envoi d’une marque F3
configurable.

L’authentification inspirée de loginPassword est volontairement désactivée
par défaut. Pour l’activer, renseigner `plugins.loginPassword.passwords` dans
`plugged.yml`, définir `loginServer` (par exemple `limbo`) et un `hubServer`.
Les joueurs sont envoyés au serveur de connexion et doivent utiliser
`/login <mot-de-passe>` avant d’être redirigés vers le hub. Ne jamais committer
ce fichier avec des mots de passe réels ; préférez un fichier de configuration
local protégé.

## Limbo externe

Le limbo n’est pas embarqué dans ce dépôt. Déployer un NanoLimbo ou PicoLimbo
externe et déclarer un serveur Gate `limbo` à l’adresse `limbo:25566`. Le
serveur principal pourra être ajouté séparément ; Gate peut alors l’utiliser
comme destination après authentification et comme repli pendant les
redémarrages.

Les projets externes restent soumis à leurs licences respectives : NanoLimbo
est GPLv3 et PicoLimbo MIT.
