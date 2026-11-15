# Como contribuir com o VIII

**Idiomas:** [English](CONTRIBUTING.md) · Português (Brasil)

O VIII é um projeto open source em estágio pré-alfa para explorar evidências digitais com resultados rastreáveis e verificáveis. Advogados, profissionais de perícia, pesquisadores e desenvolvedores são bem-vindos. **Você não precisa saber programar nem falar inglês para contribuir.**

Leia o [README em português](README.pt-BR.md) para conhecer o escopo e as limitações. O projeto ainda não possui fluxos periciais validados nem garantias de segurança para casos reais.

## Escolha como contribuir

- **Descreva um fluxo de trabalho forense.** Conte o que precisa investigar, como faz isso atualmente e o que precisaria conferir antes de confiar em um resultado. Não é necessário propor uma solução técnica.
- **Abra uma issue técnica.** Relate um comportamento reproduzível, pesquise um formato ou conjunto de dados, proponha um teste ou discuta uma decisão de engenharia.
- **Melhore a documentação ou as traduções.** Esclareça conceitos, identifique afirmações ambíguas e torne as instruções mais acessíveis.
- **Proponha código.** Nesta fase, discuta implementações substanciais em uma issue antes de abrir um pull request. Melhorias pontuais de documentação e testes são bem-vindas.

No [seletor de issues](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/new/choose), escolha **Forensic workflow** (fluxo forense) ou **Technical issue** (issue técnica). Você também pode abrir uma issue em branco.

## Idiomas e participação

- **Issues e comentários podem ser escritos em qualquer idioma.** Português brasileiro, inglês e francês são especialmente bem-vindos. Não deixe de compartilhar um problema do seu domínio por não conseguir descrevê-lo tecnicamente em inglês.
- **O inglês é o idioma de coordenação técnica:** código, identificadores, testes, contratos de API, pull requests, decisões de arquitetura e tarefas de implementação escritas por mantenedores devem, em geral, usar inglês.
- Sempre que viável, os mantenedores devem **responder e pedir esclarecimentos no idioma do contribuidor**, especialmente em decisões importantes. Não há garantia de resposta imediata ou de tradução de cada comentário técnico.
- Um mantenedor pode adicionar **um resumo técnico em inglês à issue original** para permitir a participação de desenvolvedores de outros países. Esse resumo é uma interpretação de trabalho; não substitui o relato original.
- Se houver uma tarefa de implementação independente, o mantenedor pode criar **uma issue técnica em inglês, vinculada à original nos dois sentidos**. Não haverá duplicação automática de issues traduzidas. Decisões e avanços importantes devem continuar visíveis na issue original.
- Ferramentas de tradução e redação podem auxiliar mantenedores, mas **a responsabilidade pela interpretação e correção permanece humana**. O projeto ainda não oferece tradução como serviço automatizado.

Termos jurídicos e periciais, como *vestígio*, *indício*, *prova*, *proveniência* e *cadeia de custódia*, dependem de contexto. Se a tradução mudar a interpretação, é melhor pedir esclarecimentos do que presumir requisitos.

## O que descrever em uma issue?

Em um **fluxo forense**, explique a pergunta que precisa responder, o tipo de material envolvido (por exemplo, uma exportação UFDR), a dificuldade atual e como seria possível verificar a resposta. Linguagem cotidiana é suficiente. Não é necessário enviar arquivos, telas ou código.

Em uma **issue técnica**, descreva o problema ou a pergunta de pesquisa, o resultado esperado e, quando houver, passos de reprodução, exemplos sintéticos, restrições e formas de validação. Diferencie fatos confirmados de hipóteses. Uma pergunta de pesquisa também é uma contribuição válida.

O mantenedor poderá pedir detalhes, incluir um resumo técnico em inglês, delimitar o escopo ou vincular uma tarefa de implementação separada. Um resumo traduzido não significa que o profissional que relatou o problema aprovou automaticamente a interpretação técnica.

## Proteja pessoas e materiais de casos

**Nunca publique evidências de casos reais nem informações sensíveis em issues, pull requests, arquivos de teste ou capturas de tela públicas.** Isso inclui processos sob sigilo, dados de clientes, comunicações protegidas por sigilo profissional, dados pessoais identificáveis, credenciais e exportações forenses não públicas.

Use exemplos inteiramente sintéticos ou materiais publicamente disponíveis cuja divulgação seja autorizada e cujas condições de distribuição tenham sido verificadas. Um link público para download **não é**, por si só, licença para redistribuir um dataset. Prefira indicar a fonte original, atribuição, checksums e instruções de reprodução, sem copiar bases externas para o repositório.

**Não publique detalhes exploráveis de vulnerabilidades em issues públicas.** O processo de reporte privado ainda não foi documentado; enquanto ele é definido, não compartilhe detalhes sensíveis de segurança nas discussões públicas do GitHub.

O VIII ainda não está pronto para o processamento de casos reais. As contribuições não devem apresentar funcionalidades planejadas como implementadas, validadas ou certificadas.

## Pull requests

- Vincule uma issue quando a mudança envolver código substancial ou arquitetura, mantendo um escopo revisável.
- Explique o que mudou, por quê e como foi verificado. Registre limitações ou hipóteses ainda abertas.
- Prefira testes reproduzíveis com dados sintéticos e rastreabilidade entre entrada e resultado, quando aplicável.
- Use inglês nos artefatos técnicos, conforme a convenção do projeto. Se precisar de ajuda para formular uma proposta técnica, comece por uma issue em seu idioma.
- Evite introduzir dependências, pressupostos de licenciamento ou afirmações de correção pericial sem justificativa.

Sempre que possível, os mantenedores conduzem decisões técnicas em inglês e retornam as conclusões mais relevantes ao contribuidor no idioma original. Este fluxo é deliberadamente simples e pode mudar conforme o projeto amadurecer; não representa garantia de atendimento, tradução ou prazo de resposta.
