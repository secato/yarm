# YARM

YARM installs ReShade, shader packs and add-ons into the folders of a user's games and takes
them out again, leaving the folder as it was. This glossary fixes the words used for that.

## Language

### Games

**Game**:
A title the user can install ReShade into, found by a provider or added by hand.
_Avoid_: App, title, entry

**Game folder**:
A directory holding a game's executable, which is where ReShade is installed. A game can have several.
_Avoid_: Install path, root, directory

**Provider**:
A store or launcher YARM reads to find games: Steam, GOG, Epic, Battle.net, plus manually added folders. On Linux, GOG, Epic and Battle.net are reached through Heroic and Lutris.
_Avoid_: Source, store, scanner

**Manual game**:
A game the user added by choosing its folder rather than a provider finding it.
_Avoid_: Custom game, user game

### Content

**ReShade**:
The injector itself, in a chosen version and flavor.

**Flavor**:
Whether ReShade is the normal build or the add-on-support build. Add-ons only work with the add-on-support build.
_Avoid_: Edition, variant

**Shader pack**:
A set of effects ReShade loads, such as SweetFX or iMMERSE.
_Avoid_: Package, preset, effect pack

**Add-on**:
A plugin ReShade loads at runtime, such as a RenoDX mod.
_Avoid_: Plugin, extension

**Curated list**:
The hand-picked subset of shader packs and add-ons shown first because people actually use them.
_Avoid_: Popular, recommended

**Custom content**:
Shaders and add-ons the user placed in YARM's own folder themselves, shown beside the catalog.
_Avoid_: User content, local files

**Resource**:
Anything YARM can download and keep for reuse: a ReShade version, a shader pack, an add-on.
_Avoid_: Asset, download, artifact

**Cache**:
The store of downloaded resources, shared by every game and safe to delete.

### Installing

**Install**:
The set of files YARM has put into one game folder, recorded so it can be removed again.
_Avoid_: Setup, deployment

**Registry**:
YARM's record of every install, with each file and its hash. Losing it orphans the installs.
_Avoid_: Database, manifest file

**Plan**:
What an apply is going to do, worked out before anything is written: what to download, write, replace and keep.
_Avoid_: Diff, dry run

**Apply**:
Carrying out a plan for a game: installing, changing or removing. It can be cancelled and then rolls back.
_Avoid_: Run, deploy, execute

**Uninstall**:
Removing exactly the files YARM wrote and restoring anything it replaced.
_Avoid_: Clean, remove all

**Adopt**:
Taking over a ReShade the user set up by hand, so YARM tracks and can remove it without changing any file.
_Avoid_: Import, claim

**Proxy DLL**:
The DLL ReShade is installed as so the game loads it. Only one injector can hold that name in a game folder.
_Avoid_: Hook DLL, loader

**Slot conflict**:
Another injector already holds the proxy DLL's name in the game folder.
_Avoid_: Collision, clash

**Backup**:
The original copy of a file YARM replaced, kept so uninstall can restore it.
