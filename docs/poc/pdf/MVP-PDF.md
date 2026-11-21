
# MVP do VIII — digestor local de PDFs de até 30 mil páginas

Implemente no `viii-digital-evidence-workbench` o menor fluxo completo em que um advogado importa um PDF grande, começa a pesquisar enquanto ele é processado e faz uma pergunta cuja resposta aponta para as páginas originais. O processamento padrão deve funcionar após instalar o VIII, via **Bacalhau + WASI**, sem Docker. A opção **Acelerar com Docker** deve usar Bacalhau no mesmo computador e aproveitar as páginas já processadas.

## Comece pelo risco técnico

Leia a ADR-0001, o relatório do PoC distribuído e o código atual de importação, execução e persistência. Reutilize o adaptador Bacalhau, a política de acesso à evidência e o registro de execução.

Na versão de Bacalhau fixada pelo repo, prove primeiro que um worker WASI lê um arquivo local no Windows, faz OCR de uma imagem em português e devolve um resultado que o VIII consegue persistir. Em seguida, prove que o caminho WASI consegue extrair texto e renderizar páginas selecionadas de um PDF em disco com uso de memória adequado ao tamanho do documento. Tesseract com `por.traineddata` é o primeiro candidato. Um build que só funciona em Wasmtime ainda precisa passar no executor do Bacalhau. Se a primeira biblioteca falhar, teste a alternativa mais simples antes de concluir que há um bloqueio.

## Implemente o fluxo

1. **Importar.** Registre o identificador e o SHA-256 do original. Autorize a execução no nó local antes de montar o PDF em somente leitura. Os jobs recebem uma referência local ao arquivo e as páginas a processar. Grave o texto extraído como artefato local, fora dos logs e dos argumentos do job.
2. **Processar.** Extraia o texto existente e publique os lotes concluídos enquanto o OCR continua em segundo plano. Renderize uma página por vez quando a camada de texto estiver ausente ou insuficiente. Ambos os workers entregam o mesmo formato de resultado por página, com número físico no PDF, texto, origem do texto e status. Registre também engine, versão da ferramenta e ID da execução.
3. **Retomar e acelerar.** Use faixas de páginas como unidade de trabalho e comece com um job WASI ativo por documento. Persista resultados de forma idempotente por PDF e página. Reconcile jobs já submetidos ao reabrir o VIII e processe apenas o que falta. Ao escolher Docker, preserve os resultados confirmados e resolva os jobs WASI em andamento antes de reatribuir as páginas pendentes. Se o Docker estiver indisponível, mostre como habilitá-lo e mantenha o caminho WASI disponível.
4. **Pesquisar e perguntar.** Reutilize a busca local existente ou use SQLite FTS5 se faltar uma opção adequada. Mostre quantas páginas estão pesquisáveis, pendentes ou falharam. Para uma pergunta como “Onde aparece X, e o que dizem esses trechos?”, recupere as páginas relevantes e use um único backend de inferência local. A interface monta as citações a partir das referências recuperadas, abre o PDF na página certa e identifica quando a resposta usa um índice parcial ou quando faltam trechos para responder.
5. **Distribuir.** Inclua o worker WASI e o modelo de OCR em português no empacotamento disponível do VIII. Verifique o primeiro uso com a rede desligada e sem Docker. O pré-requisito da opção acelerada é um nó local com Docker configurado. Mantenha Bacalhau como único orquestrador. O escopo deste MVP é uma workstation e um PDF por vez.

## Critérios de aceite

- Uma fixture pública ou sintética com páginas digitais e escaneadas funciona do início ao fim nos dois engines. A busca encontra um termo presente em uma página escaneada perto do fim, e a resposta citada abre exatamente essa página no original.
- É possível pesquisar páginas já confirmadas enquanto o restante do PDF continua em processamento. Páginas ilegíveis ou com erro aparecem no progresso.
- Interromper durante um lote e reabrir o VIII preserva os resultados confirmados. Trocar de WASI para Docker termina o mesmo documento sem duplicar páginas nem sobrescrever resultados já aceitos. O hash do original permanece igual.
- Execute os dois caminhos com um PDF misto representativo de 30 mil páginas. Registre tamanho, proporção de páginas com OCR, tempo até a primeira busca útil, vazão de OCR, tempo total, pico de RAM e disco temporário. Compare 1, 2 e 4 jobs WASI em um trecho representativo e observe a responsividade da máquina. Só mude a concorrência padrão se a medição justificar.
- Entregue os testes de regressão para retomada, troca de engine e referências de página, mais os comandos para repetir o teste grande. Se alguma execução ficar pendente, informe exatamente qual e por quê. Se um backend local de IA exigir configuração própria, documente esse pré-requisito e valide pelo menos uma pergunta de ponta a ponta com um modelo real.
