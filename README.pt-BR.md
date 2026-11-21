# VIII: Digital Evidence Workbench

**Idioma:** [English](README.md) · Português (Brasil) · [Français](README.fr.md)

**Uma plataforma de código aberto em desenvolvimento para explorar evidências digitais, compreender o que elas mostram e rastrear os achados até suas fontes.**

Evidências digitais raramente chegam em um formato conveniente. Um único caso pode envolver milhares de páginas, conversas exportadas, registros telefônicos, arquivos de áudio e relatórios de diferentes ferramentas. Encontrar algo relevante é apenas parte do trabalho: também é preciso saber **de onde veio, como foi processado e quais são suas limitações**.

O VIII (Digital Evidence Workbench) é uma iniciativa em estágio inicial para tornar esse trabalho mais acessível a equipes jurídicas, mais reproduzível para profissionais de perícia e mais extensível para quem desenvolve software.

> [!IMPORTANT]
> **Situação do projeto: pré-alfa.** O VIII está em fase de definição, com duas frentes experimentais que funcionam com dados sintéticos. Ainda não é um produto pericial utilizável: não há versões estáveis, fluxos periciais validados ou garantias de segurança para uso em produção. **Não envie nem processe materiais de casos reais com este projeto nesta fase.**

## O que já existe

Duas frentes experimentais, testadas em uma estação de trabalho Windows **somente com dados sintéticos**:

- **PDFs grandes (experimental).** Importe um PDF de até 30 mil páginas, pesquise as páginas à medida que são processadas, faça OCR em português das páginas digitalizadas e pergunte a um modelo local (llama.cpp) cujas citações abrem a página física do original. As tarefas rodam pelo Bacalhau com módulos WASI por padrão, sem exigir Docker; o Docker é uma aceleração opcional. PDFs grandes são divididos em partes verificadas de 40 páginas, e todo resultado aponta para o original, que permanece imutável. Em um arquivo sintético de 30 mil páginas e 1,25 GB, com 3.001 páginas digitalizadas, o fluxo completo levou 6 h 04 min com WASI e 2 h 30 min com Docker, sem páginas com falha. Comece pela [documentação do PDF](docs/poc/pdf/README.md) (em inglês).
- **Mensagens UFDR (prova de conceito).** Um módulo WASI, sem Docker, normaliza um arquivo sintético de mensagens UFDR com referências às fontes, e uma versão de desenvolvimento do aplicativo de desktop exibe o resultado. Consulte o [relatório de validação](docs/poc/issue-9-validation.md) e a [issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9).

Nenhuma das frentes foi avaliada com evidências reais, instalada por usuários finais em uma máquina limpa ou revisada por profissionais de perícia.

## O que você poderá fazer com o VIII?

O objetivo é ajudar a passar de *um conjunto de arquivos* para *uma investigação que possa ser explicada e verificada*.

Estes são alguns **exemplos de fluxos de trabalho que pretendemos oferecer**. Somente o fluxo de documentos digitalizados existe, como a frente experimental de PDF descrita acima:

| Ponto de partida | Perguntas a investigar | Resultado pretendido |
| --- | --- | --- |
| Uma exportação UFDR XML | Quais conversas, contatos ou eventos merecem uma análise mais detalhada? | Registros pesquisáveis e achados vinculados às fontes |
| Um conjunto de documentos digitalizados de um caso | Onde uma pessoa, data ou evento é mencionado? | Texto extraído com links para as páginas originais |
| Gravações de áudio e registros de chamadas | O que foi dito, quando e em qual gravação? | Transcrições pesquisáveis com referências de tempo e indicação de incertezas |
| Evidências de várias fontes | Os horários e os relatos são compatíveis? | Uma cronologia rastreável dos eventos relevantes |

**A automação deve auxiliar a investigação, sem formular conclusões probatórias por conta própria.** Os resultados precisam permanecer disponíveis para revisão por pessoas com a qualificação adequada.

## Para quem é o projeto?

**Profissionais de perícia:** ajudem a definir fluxos de trabalho que preservem o contexto, explicitem premissas e tornem o processamento reproduzível. Seu conhecimento sobre as falhas das ferramentas é tão útil quanto o código.

**Advogados e equipes jurídicas:** ajudem a definir perguntas práticas, relatórios úteis e explicações compreensíveis sobre as limitações técnicas. Não é preciso saber programar para participar.

**Desenvolvedores e pesquisadores:** ajudem a criar analisadores de formatos, módulos de processamento, testes, interoperabilidade e a infraestrutura da plataforma, com prioridade para a execução local.

## Princípios de projeto

- **Evidências em primeiro lugar.** Preservar o material original; tratar textos extraídos, transcrições, índices e resultados de IA como artefatos derivados.
- **Rastreabilidade por padrão.** Os achados devem apontar para suas fontes, com metadados de processamento suficientes para entender como foram produzidos.
- **Controle local.** O usuário deve poder escolher onde o processamento ocorre e quais dados, se houver, saem de seu ambiente.
- **A responsabilidade continua sendo humana.** Uma ferramenta útil deve explicitar incertezas e apoiar a revisão especializada, sem substituir o julgamento de profissionais qualificados em perícia ou direito.
- **Aberto ao escrutínio.** Priorizar formatos documentados, testes reproduzíveis e fluxos de trabalho que especialistas possam examinar e questionar.
- **Extensível sem configuração desnecessária.** Um advogado não deveria precisar entender Docker ou sistemas distribuídos para usar o futuro aplicativo de desktop.

## Por onde o projeto começa

O primeiro escopo planejado é deliberadamente restrito: **um fluxo de trabalho local para evidências digitais estruturadas, começando pelo UFDR XML**.

Por que começar por aí? Esse formato oferece um ponto de partida concreto e perguntas úteis para investigação, sem exigir reconhecimento óptico de caracteres (OCR), reconhecimento de fala ou infraestrutura de GPU desde o início.

Marcos iniciais propostos:

1. Definir um modelo para evidências, artefatos derivados, proveniência e histórico de processamento.
2. Analisar localmente um pequeno arquivo sintético de teste em UFDR XML e gerar resultados que possam ser inspecionados e vinculados às fontes.
3. Estabelecer testes repetíveis e um tratamento claro para registros não suportados ou ambíguos.
4. Explorar uma interface de desktop e uma instalação que dispense Docker.
5. Avaliar a execução remota opcional e fluxos mais exigentes de processamento de documentos e áudio.

A prova de conceito de UFDR cobre versões iniciais dos marcos 1 a 3 e uma versão de desenvolvimento do aplicativo de desktop; a frente de PDF antecipou o OCR e foi validada sem Docker. A instalação em uma máquina limpa, as atualizações, a execução entre máquinas e a validação por profissionais continuam pendentes ([issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9), [issue #11](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/11)). A ordem pode mudar conforme os profissionais validarem os fluxos de trabalho.

## Onde o processamento acontece

O objetivo de longo prazo é **uma única plataforma com três modos de execução**, e não três produtos diferentes:

| Modo | O que significa | Limite importante |
| --- | --- | --- |
| **Nuvem** | Executar tarefas autorizadas em infraestrutura gerenciada | A transferência de dados, os acessos e os custos devem ser explícitos |
| **Híbrido** | Manter determinadas etapas locais e executar outras remotamente | Somente dados de entrada especificamente autorizados devem sair do dispositivo |
| **Local / isolado da rede (air-gapped)** | Executar todo o fluxo suportado em uma máquina isolada | Modelos, dependências, licenças e atualizações devem funcionar sem rede |

**O isolamento de rede é uma propriedade de segurança a ser verificada, não uma opção de marketing.** Essa capacidade só será anunciada quando for possível testar a instalação e o comportamento sem conexão.

### Direção técnica

A arquitetura separa a lógica pericial de *onde* uma tarefa é executada. As decisões atuais estão registradas em [registros de decisão de arquitetura](docs/adr) (em inglês):

```text
Aplicativo de desktop                   Go + Wails v3 (ADR-0002, proposta)
  ├── Casos e evidências                originais imutáveis, identidade SHA-256
  ├── Achados, busca e proveniência     índice local; todo resultado aponta para a fonte
  └── Processamento                     Bacalhau como único orquestrador (ADR-0001, aceita)
       ├── Módulos WASI                 padrão portável, sem exigir Docker
       └── Módulos Docker               opcionais, para desempenho ou dependências nativas
```

- **Um único ciclo de vida das tarefas, local ou remoto.** O [Bacalhau](https://github.com/bacalhau-project/bacalhau) executa todas as tarefas de processamento, hoje na estação de trabalho e, depois, em nós remotos autorizados. Tarefas nativas executadas fora do Bacalhau ficam fora do produto; a frente de PDF já segue essa regra, mas a versão de desenvolvimento de UFDR ainda oferece a execução nativa como padrão e precisa ser alinhada. Até agora, a execução entre máquinas só foi exercitada em uma única máquina física.
- **Documentos grandes são processados em partes verificadas.** Os PDFs são divididos por um módulo WASI antes do OCR; as partes são cópias operacionais vinculadas ao original e nunca são tratadas como evidência ([ADR-0003](docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md)).
- **A inferência é local e opcional.** As perguntas usam um modelo local instalado à parte; a busca e o OCR funcionam sem ele.

A operação isolada da rede continua sendo um objetivo: o primeiro uso com rede restrita passou na estação de desenvolvimento, mas a instalação e as atualizações sem conexão ainda não foram verificadas.

## Ajude a definir o VIII

Contribuições são bem-vindas em qualquer idioma. Consulte [Como contribuir](CONTRIBUTING.pt-BR.md) ou [Contributing](CONTRIBUTING.md) e use o [seletor de issues](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/new/choose). A coordenação técnica ocorre, em geral, em inglês; quando viável, os mantenedores retornam os avanços relevantes no idioma original do contribuidor.

**Você não precisa escrever código.** Nesta fase, uma descrição clara de um fluxo de trabalho difícil pode ser mais valiosa do que um pull request extenso.

Alguns pontos de partida úteis:

| Se você é... | Uma primeira contribuição valiosa |
| --- | --- |
| Profissional de perícia | Descreva um formato de exportação, uma armadilha de interpretação ou uma etapa de validação que as ferramentas atuais não atendem bem |
| Advogado | Descreva uma pergunta recorrente sobre evidências digitais e o que seria necessário para verificar a resposta |
| Desenvolvedor | Proponha um pequeno teste de análise de formato, um arquivo sintético de teste, uma interface de processamento ou uma verificação de reprodutibilidade |
| Pesquisador ou educador | Sugira uma referência de avaliação pública ou sintética, uma metodologia ou uma melhoria na documentação |

**Comece uma conversa:** [abra uma issue](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues) descrevendo o problema, o tipo de material envolvido, como seria um resultado correto e por que as ferramentas existentes não atendem à necessidade. Propostas e correções são bem-vindas; pull requests com implementações substanciais devem ser discutidos antes, enquanto a arquitetura ainda está sendo definida.

> [!CAUTION]
> **Não publique evidências reais, documentos de processos sob sigilo, dados de clientes, comunicações protegidas por sigilo profissional, credenciais ou dados pessoais que permitam identificar alguém** em issues ou pull requests públicos. Use exemplos sintéticos ou materiais cuja publicação você esteja autorizado a fazer. Não divulgue publicamente detalhes de segurança que possam ser explorados; instruções para relatos privados serão estabelecidas antes do lançamento de versões.

## O que o VIII *não* é

O VIII **não** é uma certificação pericial, um exame pericial oficial, um parecer jurídico ou um substituto para a revisão por especialistas qualificados. O software, por si só, não pode estabelecer a autenticidade, a admissibilidade ou o valor probatório de um material. Essas questões dependem de metodologia, contexto, legislação aplicável e julgamento profissional.

## Licença e gestão do projeto

**O núcleo do projeto é licenciado sob a MPL 2.0** ([LICENSE](LICENSE)). Os avisos de terceiros referentes às ferramentas e aos modelos usados pelos módulos de processamento (por exemplo, Bacalhau, Tesseract e seu modelo de português, e flpdf) ainda precisam ser reunidos antes de qualquer versão.

O projeto começa sob a responsabilidade de [`vmenezesdev`](https://github.com/vmenezesdev). A intenção é manter o núcleo aberto à inspeção e utilizável tanto em atividades não comerciais quanto comerciais, incentivando que melhorias distribuídas nos arquivos de código-fonte cobertos pela MPL permaneçam abertas nos termos da licença.

A [Luminiware](https://luminiware.com) poderá oferecer serviços comerciais opcionais relacionados ao projeto no futuro, como infraestrutura gerenciada, implantação, suporte, revisão humana preliminar e acesso a especialistas independentes em perícia. **Essas são propostas de serviços, não ofertas disponíveis anunciadas por este repositório.** O projeto aberto e os contratos comerciais serão documentados separadamente.

---

**A primeira pergunta não é “Quanto podemos automatizar?”, mas “O que os profissionais precisam ser capazes de verificar?”.**

Se você trabalha com evidências digitais, [conte qual fluxo de trabalho deveria vir primeiro](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues).
