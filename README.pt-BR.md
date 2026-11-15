# VIII: Digital Evidence Workbench

**Idioma:** [English](README.md) · Português (Brasil) · [Français](README.fr.md)

**Uma plataforma de código aberto em desenvolvimento para explorar evidências digitais, compreender o que elas mostram e rastrear os achados até suas fontes.**

Evidências digitais raramente chegam em um formato conveniente. Um único caso pode envolver milhares de páginas, conversas exportadas, registros telefônicos, arquivos de áudio e relatórios de diferentes ferramentas. Encontrar algo relevante é apenas parte do trabalho: também é preciso saber **de onde veio, como foi processado e quais são suas limitações**.

O VIII (Digital Evidence Workbench) é uma iniciativa em estágio inicial para tornar esse trabalho mais acessível a equipes jurídicas, mais reproduzível para profissionais de perícia e mais extensível para quem desenvolve software.

> [!IMPORTANT]
> **Situação do projeto: concepção / pré-alfa.** O VIII é uma proposta de projeto em fase de definição, ainda sem um produto pericial utilizável. Não há versões estáveis, fluxos de trabalho validados ou garantias de segurança para uso em produção. **Não envie nem processe materiais de casos reais com este projeto nesta fase.**

## O que você poderá fazer com o VIII?

O objetivo é ajudar a passar de *um conjunto de arquivos* para *uma investigação que possa ser explicada e verificada*.

Estes são alguns **exemplos de fluxos de trabalho que pretendemos oferecer**, não funcionalidades disponíveis hoje:

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

A ordem pode mudar conforme os profissionais validarem os fluxos de trabalho.

## Onde o processamento acontece

O objetivo de longo prazo é **uma única plataforma com três modos de execução**, e não três produtos diferentes:

| Modo | O que significa | Limite importante |
| --- | --- | --- |
| **Nuvem** | Executar tarefas autorizadas em infraestrutura gerenciada | A transferência de dados, os acessos e os custos devem ser explícitos |
| **Híbrido** | Manter determinadas etapas locais e executar outras remotamente | Somente dados de entrada especificamente autorizados devem sair do dispositivo |
| **Local / isolado da rede (air-gapped)** | Executar todo o fluxo suportado em uma máquina isolada | Modelos, dependências, licenças e atualizações devem funcionar sem rede |

**O isolamento de rede é uma propriedade de segurança a ser verificada, não uma opção de marketing.** Essa capacidade só será anunciada quando for possível testar a instalação e o comportamento sem conexão.

### Direção técnica (em avaliação)

A arquitetura proposta separa a lógica pericial de *onde* uma tarefa é executada:

```text
Aplicativo de desktop
  ├── Espaço de trabalho para casos e evidências
  ├── Achados, busca e proveniência
  └── Interface de processamento
       ├── Módulos nativos locais         (sem exigir Docker)
       └── Execução distribuída de tarefas (quando autorizada)
            └── Bacalhau — ambiente de execução em avaliação
                 ├── Tarefas portáveis, quando viável
                 └── Módulos em contêineres para tarefas mais exigentes
```

O [Bacalhau](https://github.com/bacalhau-project/bacalhau) está sendo avaliado para execução distribuída. Rust/Tauri, Go, ferramentas de processamento baseadas em Python, WASM e fluxos de OCR são **opções de implementação**, não dependências definitivas ou integrações já disponíveis.

Manter os módulos de processamento por trás de uma interface estável deve permitir a execução local das tarefas adequadas sem instalar Docker, além do uso de contêineres em nós de processamento remoto quando fizer sentido.

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

**A MPL 2.0 é a licença planejada para o núcleo do projeto.** Os arquivos de licença, os avisos de terceiros e as orientações de contribuição serão adicionados antes da primeira versão do código-fonte. Até lá, não presuma autorização para reutilizar código não publicado ou sem licença.

O projeto começa sob a responsabilidade de [`vmenezesdev`](https://github.com/vmenezesdev). A intenção é manter o núcleo aberto à inspeção e utilizável tanto em atividades não comerciais quanto comerciais, incentivando que melhorias distribuídas nos arquivos de código-fonte cobertos pela MPL permaneçam abertas nos termos da licença.

A [Luminiware](https://luminiware.com) poderá oferecer serviços comerciais opcionais relacionados ao projeto no futuro, como infraestrutura gerenciada, implantação, suporte, revisão humana preliminar e acesso a especialistas independentes em perícia. **Essas são propostas de serviços, não ofertas disponíveis anunciadas por este repositório.** O projeto aberto e os contratos comerciais serão documentados separadamente.

---

**A primeira pergunta não é “Quanto podemos automatizar?”, mas “O que os profissionais precisam ser capazes de verificar?”.**

Se você trabalha com evidências digitais, [conte qual fluxo de trabalho deveria vir primeiro](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues).
