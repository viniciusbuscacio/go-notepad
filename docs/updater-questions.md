# Updater — perguntas de alinhamento

Responda abaixo de cada pergunta. Rascunho de trabalho; não precisa commitar.

## Comportamento da checagem

**1.** Quando checar: só manual (botão "Check for updates"), automático no startup, ou os dois?

R:

**2.** Se tiver checagem automática: roda em toda abertura do app ou com intervalo mínimo (ex.: no máximo 1x por dia)?

R:

**3.** A checagem automática vem ligada ou desligada de fábrica? (hoje os apps não fazem nenhuma chamada externa sem o usuário pedir — isso mudaria)

R:

**4.** Builds de desenvolvimento (appVersion "dev", sem tag): a checagem fica desabilitada nesses builds?

R:

**5.** Pre-releases (tags beta/rc) contam como update disponível, ou só releases estáveis?

R:

## Aviso ao usuário

**6.** Onde o "update disponível" aparece: badge/ponto na engrenagem da title bar, toast, seção dentro de Settings, ou uma combinação?

R:

**7.** O aviso mostra as release notes do GitHub dentro do app, ou só o número da versão nova?

R:

**8.** "Pular esta versão": pula só aquela tag (volta a avisar quando sair outra) ou silencia tudo até o usuário checar manualmente?

R:

**9.** "Depois": lembra de novo na próxima abertura, ou aplica um cooldown (ex.: só volta a avisar em 3 dias)?

R:

**10.** Idioma da UI de update: inglês, seguindo o padrão da família?

R:

## Instalação do update

**11.** O botão "Instalar" já faz self-update de verdade (baixa e troca o binário sozinho), ou na v1 ele abre a página do release no navegador e o self-update fica pra v2?

R:

**12.** Se self-update: ao terminar, reinicia o app automaticamente ou pergunta ("Restart now / Restart later")?

R:

**13.** O download do binário novo começa em background enquanto o usuário decide, ou só depois do clique em "Instalar"?

R:

**14.** Verificação de integridade: publicamos um checksums.txt nos releases (mexe no release.yml) e o app confere antes de trocar o binário?

R:

**15.** Se a troca do binário falhar (sem permissão na pasta, antivírus segurando): fallback abre a página do release no navegador, ou só mostra mensagem de erro?

R:

## Integração com o resto do app

**16.** Expor o updater na API REST / árvore `/v1/ax` (ex.: controle check-updates com `risk: external`), pros agentes verem e testarem?

R:

**17.** O smoke test (`tools/smoke`) ganha checks do updater (testids na UI, endpoint mockado)?

R:

**18.** As preferências novas (auto-check on/off, versão pulada) entram no JSON do `internal/settings` existente, certo?

R:

## Estratégia da família

**19.** Implementamos primeiro no go-notepad e portamos pro go-calc copiando o `internal/updater` (padrão atual da família), ou já criamos um módulo Go compartilhado entre os repos?

R:

**20.** Release de estreia: assim que o updater entrar, publicamos um v0.2.0 e depois um v0.2.1 pequeno só pra testar o ciclo completo de update na prática?

R:
