# Gate Plugged

Passerelle Minecraft basée sur [Gate](https://github.com/minekube/gate),
conçue pour centraliser les plugins et connecter un proxy à un serveur
Pelican. Le projet ajoute notamment la détection de clients/mods, le vanish,
des commandes utilitaires et l'intégration Pelican.

## Fonctionnalités

- intégration Pelican pour démarrer et arrêter les serveurs à la demande ;
- détection des mods Forge/NeoForge et des clients Lunar ;
- commande `/vanish` avec gestion des permissions ;
- commandes proxy personnalisées et marqueur F3 configurable ;
- rechargement automatique de la configuration Gate.

## Démarrage avec Podman ou Docker

Le dépôt contient un `Dockerfile` multi-stage. Avec Podman :

```bash
podman build --network=host -t gate-plugged:local .
podman run --rm --network=host \
  -e GATE_VELOCITY_SECRET="votre-secret" \
  gate-plugged:local
```

Avec Docker, remplacer `podman` par `docker` et utiliser le réseau adapté à
votre environnement. Le proxy écoute par défaut sur `0.0.0.0:25565` et tente
de joindre le serveur `limbo` sur `limbo:25566`.

## Configuration

- `config.yml` contient la configuration du proxy Gate ;
- `plugged.yml` contient la configuration des plugins ;
- les valeurs peuvent être surchargées par variables d'environnement ;
- le secret de forwarding Velocity doit être fourni via
  `GATE_VELOCITY_SECRET` ;
- le token et l'URL Pelican doivent être remplacés dans `plugged.yml` ou
  surchargés par les variables `PLUGGED_*` correspondantes.

Le sous-module `HackedServer` est requis pour les ressources de détection :

```bash
git submodule update --init --recursive
```

## Développement

Prérequis : Go 1.26 et le sous-module initialisé.

```bash
go test ./...
go run ./gate.go
```

Les contrôles disponibles via le Makefile sont `fmt`, `vet`, `mod`, `lint` et
`test`.

## Licence

Distribué sous licence MIT. Voir [`LICENSE`](LICENSE).
