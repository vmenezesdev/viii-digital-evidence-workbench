# VIII: Digital Evidence Workbench

**Langue :** [English](README.md) · [Português (Brasil)](README.pt-BR.md) · Français

**Un environnement de travail open source en cours de développement pour explorer les éléments de preuve numériques, comprendre ce qu’ils révèlent et remonter des constats à leurs sources.**

Les éléments de preuve numériques se présentent rarement sous une forme pratique. Un seul dossier peut comprendre des milliers de pages, des conversations exportées, des relevés téléphoniques, des fichiers audio et des rapports issus de différents outils. Trouver une information pertinente ne constitue qu’une partie du travail : il faut aussi savoir **d’où elle provient, comment elle a été traitée et quelles sont ses limites**.

VIII (Digital Evidence Workbench) est une initiative à ses débuts qui vise à rendre ce travail plus accessible aux équipes juridiques, plus reproductible pour les professionnels de l’investigation numérique et plus extensible pour les développeurs.

> [!IMPORTANT]
> **État du projet : pré-alpha.** VIII est en cours de définition, avec deux volets expérimentaux qui fonctionnent sur des données synthétiques. Ce n’est pas encore un outil d’expertise numérique utilisable : il n’existe ni version stable, ni processus d’expertise validé, ni garantie de sécurité pour une utilisation en production. **À ce stade, ne téléversez et ne traitez aucun document ni aucune donnée provenant de dossiers réels avec ce projet.**

## Ce qui existe aujourd’hui

Deux volets expérimentaux, testés sur un poste de travail Windows **uniquement avec des données synthétiques** :

- **PDF volumineux (expérimental).** Importez un PDF de 30 000 pages au maximum, recherchez dans les pages dès qu’elles sont traitées, appliquez l’OCR en portugais aux pages numérisées et interrogez un modèle local (llama.cpp) dont les citations ouvrent la page physique de l’original. Les tâches s’exécutent via Bacalhau avec des modules WASI par défaut, sans nécessiter Docker ; Docker est une accélération facultative. Les PDF volumineux sont découpés en segments vérifiés de 40 pages, et chaque résultat renvoie à l’original, qui reste inchangé. Sur un fichier synthétique de 30 000 pages et 1,25 Go, dont 3 001 pages numérisées, le traitement complet a duré 6 h 04 min avec WASI et 2 h 30 min avec Docker, sans page en échec. Commencez par la [documentation PDF](docs/poc/pdf/README.md) (en anglais).
- **Messages UFDR (preuve de concept).** Un module WASI, sans Docker, normalise un jeu de messages UFDR synthétique avec des références aux sources, et une version de développement de l’application de bureau affiche le résultat. Voir le [rapport de validation](docs/poc/issue-9-validation.md) et l’[issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9).

Aucun des deux volets n’a été évalué sur de vraies preuves, installé par des utilisateurs finaux sur une machine vierge ni examiné par des professionnels de l’investigation numérique.

## Que pourrez-vous faire avec VIII ?

L’objectif est de vous aider à passer *d’un ensemble de fichiers* à *une investigation que vous pouvez expliquer et vérifier*.

Voici quelques **exemples de processus que nous souhaitons prendre en charge**. Seul le traitement des documents numérisés existe, sous la forme du volet PDF expérimental décrit plus haut :

| Point de départ | Questions à examiner | Résultat visé |
| --- | --- | --- |
| Un export UFDR XML | Quelles conversations, quels contacts ou quels événements méritent un examen approfondi ? | Des données consultables par recherche et des constats liés à leurs sources |
| Un ensemble de documents numérisés d’un dossier | Où une personne, une date ou un événement sont-ils mentionnés ? | Du texte extrait avec des liens vers les pages d’origine |
| Des enregistrements audio et des relevés d’appels | Qu’a-t-on dit, quand et dans quel enregistrement ? | Des transcriptions consultables par recherche, avec des repères temporels et les incertitudes signalées |
| Des éléments provenant de plusieurs sources | Les horodatages et les récits concordent-ils ? | Une chronologie traçable des événements pertinents |

**L’automatisation doit faciliter l’investigation, sans tirer à elle seule de conclusions sur la valeur probante des éléments.** Les résultats doivent pouvoir être examinés par des personnes disposant de l’expertise appropriée.

## À qui s’adresse ce projet ?

**Aux professionnels de l’investigation numérique :** aidez à définir des processus qui préservent le contexte, explicitent les hypothèses et rendent le traitement reproductible. Votre connaissance des défaillances des outils est aussi utile que le code.

**Aux avocats et aux équipes juridiques :** aidez à définir des questions pratiques, des rapports utiles et des explications compréhensibles des limites techniques. Il n’est pas nécessaire de savoir programmer pour participer.

**Aux développeurs et aux chercheurs :** contribuez aux analyseurs de formats, aux modules de traitement, aux tests, à l’interopérabilité et à l’infrastructure privilégiant l’exécution locale.

## Principes de conception

- **Les éléments de preuve avant tout.** Préserver les originaux ; considérer les textes extraits, les transcriptions, les index et les résultats de l’IA comme des artefacts dérivés.
- **La traçabilité par défaut.** Les constats doivent renvoyer à leurs sources, avec suffisamment de métadonnées de traitement pour comprendre comment ils ont été produits.
- **La maîtrise locale.** L’utilisateur doit pouvoir choisir où s’effectue le traitement et quelles données, le cas échéant, quittent son environnement.
- **La responsabilité reste humaine.** Un outil utile doit rendre les incertitudes visibles et faciliter l’examen par des spécialistes, sans remplacer le jugement de professionnels qualifiés en expertise numérique ou en droit.
- **L’ouverture à l’examen critique.** Privilégier les formats documentés, les tests reproductibles et les processus que les spécialistes peuvent examiner et remettre en question.
- **L’extensibilité sans configuration superflue.** Un avocat ne devrait pas avoir à comprendre Docker ou les systèmes distribués pour utiliser la future application de bureau.

## Par où commence le projet ?

Le premier périmètre envisagé est volontairement restreint : **un processus local pour les éléments de preuve numériques structurés, en commençant par UFDR XML**.

Pourquoi commencer par là ? Ce format offre un point de départ concret et des questions d’investigation utiles, sans exiger dès le départ de reconnaissance optique de caractères (OCR), de reconnaissance vocale ou d’infrastructure GPU.

Premiers jalons proposés :

1. Définir un modèle pour les éléments de preuve, les artefacts dérivés, la provenance et l’historique des traitements.
2. Analyser localement un petit fichier de test synthétique au format UFDR XML et produire des résultats vérifiables, liés à leurs sources.
3. Établir des tests répétables et un traitement explicite des données non prises en charge ou ambiguës.
4. Explorer une interface de bureau et une installation ne nécessitant pas Docker.
5. Évaluer l’exécution distante facultative et les chaînes de traitement plus exigeantes pour les documents et l’audio.

La preuve de concept UFDR couvre des premières versions des jalons 1 à 3 et une version de développement de l’application de bureau ; le volet PDF a avancé l’OCR et a été validé sans Docker. L’installation sur une machine vierge, les mises à jour, l’exécution entre plusieurs machines et la validation par des professionnels restent à faire ([issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9), [issue #11](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/11)). Cet ordre pourra évoluer à mesure que les professionnels valideront les processus.

## Où s’effectue le traitement ?

L’objectif à long terme est **un seul environnement de travail avec trois modes d’exécution**, et non trois produits différents :

| Mode | Signification | Limite importante |
| --- | --- | --- |
| **Cloud** | Exécuter les tâches autorisées sur une infrastructure gérée | Les transferts de données, les accès et les coûts doivent être explicites |
| **Hybride** | Conserver certaines étapes en local et en exécuter d’autres à distance | Seules les données d’entrée expressément autorisées doivent quitter l’appareil |
| **Local / isolé du réseau (air-gapped)** | Exécuter l’intégralité du processus pris en charge sur une machine isolée | Les modèles, les dépendances, les licences et les mises à jour doivent fonctionner sans réseau |

**L’isolement du réseau est une propriété de sécurité à vérifier, pas une option marketing.** Cette capacité ne sera annoncée que lorsque l’installation et le comportement hors ligne pourront être testés.

### Orientation technique

L’architecture sépare la logique d’expertise du *lieu* d’exécution d’une tâche. Les décisions actuelles sont consignées dans des [enregistrements de décision d’architecture](docs/adr) (en anglais) :

```text
Application de bureau                   Go + Wails v3 (ADR-0002, proposée)
  ├── Dossiers et éléments de preuve    originaux inchangés, identité SHA-256
  ├── Constats, recherche, provenance   index local ; chaque résultat renvoie à sa source
  └── Traitement                        Bacalhau comme orchestrateur unique (ADR-0001, acceptée)
       ├── Modules WASI                 défaut portable, sans Docker
       └── Modules Docker               facultatifs, pour les performances ou les dépendances natives
```

- **Un seul cycle de vie des tâches, en local ou à distance.** [Bacalhau](https://github.com/bacalhau-project/bacalhau) exécute toutes les tâches de traitement, aujourd’hui sur le poste de travail et, plus tard, sur des nœuds distants autorisés. Les tâches natives exécutées hors de Bacalhau sont exclues du produit ; le volet PDF respecte déjà cette règle, mais la version de développement UFDR propose encore l’exécution native par défaut et doit être alignée. L’exécution entre plusieurs machines n’a encore été testée que sur une seule machine physique.
- **Les documents volumineux sont traités par segments vérifiés.** Les PDF sont découpés par un module WASI avant l’OCR ; les segments sont des copies opérationnelles liées à l’original et ne sont jamais traités comme des preuves ([ADR-0003](docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md)).
- **L’inférence reste locale et facultative.** Les questions utilisent un modèle local installé séparément ; la recherche et l’OCR fonctionnent sans lui.

Le fonctionnement sans réseau reste un objectif : une première utilisation avec un réseau restreint a réussi sur le poste de développement, mais l’installation et les mises à jour hors ligne n’ont pas été vérifiées.

## Aidez à définir VIII

Vous pouvez contribuer dans la langue de votre choix. Consultez [Contributing (English)](CONTRIBUTING.md) ou [Como contribuir (portugais)](CONTRIBUTING.pt-BR.md), puis [choisissez un formulaire](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/new/choose). La coordination technique se fait généralement en anglais ; dans la mesure du possible, les mainteneurs communiquent les décisions importantes dans la langue de la contribution originale.

**Vous n’avez pas besoin d’écrire du code.** À ce stade, une description claire d’un processus difficile peut être plus précieuse qu’une pull request volumineuse.

Quelques pistes pour commencer :

| Si vous êtes… | Une première contribution utile |
| --- | --- |
| Professionnel de l’investigation numérique | Décrivez un format d’export, un piège d’interprétation ou une étape de validation que les outils actuels gèrent mal |
| Avocat | Décrivez une question récurrente sur les éléments de preuve numériques et ce dont vous auriez besoin pour vérifier la réponse |
| Développeur | Proposez un petit test d’analyse de format, un fichier de test synthétique, une interface de traitement ou un contrôle de reproductibilité |
| Chercheur ou enseignant | Suggérez un jeu de référence public ou synthétique, une méthodologie ou une amélioration de la documentation |

**Lancez la discussion :** [ouvrez une issue](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues) en précisant le problème, le type de données concernées, le résultat attendu et les limites des outils existants. Les propositions et les corrections sont les bienvenues ; les pull requests apportant des implémentations importantes doivent être discutées au préalable, tant que l’architecture est en cours de définition.

> [!CAUTION]
> **Ne publiez pas d’éléments de preuve réels, de documents de procédure confidentiels, de données de clients, de communications protégées par le secret professionnel, d’identifiants d’accès ou de données personnelles permettant d’identifier quelqu’un** dans les issues ou les pull requests publiques. Utilisez des exemples synthétiques ou des contenus que vous êtes autorisé à publier. Ne divulguez pas publiquement de détails de sécurité exploitables ; des instructions de signalement privé seront établies avant la publication des premières versions.

## Ce que VIII *n’est pas*

VIII **n’est ni** une certification d’expertise numérique, ni une expertise officielle, ni un avis juridique, ni un substitut à l’examen par des spécialistes qualifiés. Le logiciel ne peut pas, à lui seul, établir l’authenticité, la recevabilité ou la valeur probante d’un élément. Ces questions dépendent de la méthodologie, du contexte, du droit applicable et du jugement professionnel.

## Licence et gouvernance du projet

**Le cœur du projet est distribué sous licence MPL 2.0** ([LICENSE](LICENSE)). Les mentions relatives aux outils et modèles tiers utilisés par les modules de traitement (par exemple Bacalhau, Tesseract et son modèle portugais, et flpdf) restent à rassembler avant toute publication.

Le projet démarre sous la responsabilité de [`vmenezesdev`](https://github.com/vmenezesdev). L’intention est de garder le cœur du projet ouvert à l’examen et utilisable à des fins aussi bien non commerciales que commerciales, tout en encourageant le maintien sous licence ouverte des améliorations distribuées des fichiers sources couverts par la MPL, conformément à ses dispositions.

[Luminiware](https://luminiware.com) pourrait proposer à l’avenir des services commerciaux facultatifs autour du projet : infrastructure gérée, déploiement, assistance, examen humain préliminaire et accès à des spécialistes indépendants en expertise numérique. **Il s’agit de propositions, pas de services disponibles annoncés par ce dépôt.** Le projet ouvert et les contrats commerciaux feront l’objet de documentations distinctes.

---

**La première question n’est pas « Jusqu’où pouvons-nous automatiser ? », mais « Que doivent pouvoir vérifier les professionnels ? ».**

Si vous travaillez avec des éléments de preuve numériques, [dites-nous quel processus devrait être prioritaire](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues).
