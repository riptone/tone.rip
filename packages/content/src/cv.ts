/* CV content - the single source of truth for every surface that shows it.

   Lives here rather than in apps/web because it is no longer one app's data:
   the site renders it at /cv, the API server-renders it for
   crawlers, and apps/ssh-cv serves it over SSH. Anything that has to agree
   about what the CV says reads it from this module.

   Fully bilingual: every field is authored per language (pt is European
   Portuguese, pt-PT), so switching language re-renders in that language and
   not just the section headings. Tech names (React, CSP, Docker…) stay
   untranslated in both.

   Two surfaces, two depths, one module. The website prints `org` - what an
   organisation does - and never `company`, which is the editorial choice
   described on the CV page and in docs/architecture.md. apps/ssh-cv prints
   the name, the per-role `detail`, and everything else here: typing
   `ssh cv.tone.rip` is a deliberate act by someone who wants the long
   version, not a search result someone landed on.

   So: `org`, `period`, `place`, `stack` and `bullets` are public on both.
   `company` and `detail` are the SSH CV's alone. A field added below has to
   be honest on whichever surfaces render it. */

/** The languages the CV is authored in. */
export type CvLang = "en" | "pt";

export interface Experience {
  role: string;
  /** What the organisation does. This is what the website prints. */
  org: string;
  /**
   * Its actual name. Printed by apps/ssh-cv, never by the website - see the
   * note at the top of this file. Optional: a role without one reads as its
   * `org` everywhere, so the name can be filled in later without a code
   * change anywhere else.
   */
  company?: string;
  period: string;
  place: string;
  /** The languages, frameworks and tools the role actually used. */
  stack: string[];
  bullets: string[];
  /**
   * The longer version: what the work involved beyond the one-line summary.
   * Only apps/ssh-cv has the room, and only it renders these.
   */
  detail?: string[];
}

export interface Education {
  title: string;
  period: string;
  bullets: string[];
}

/** A certification, and what it actually covers. */
export interface Certification {
  title: string;
  note: string;
}

/** A ranked "what I'm best at" row: a short key and its one-line expansion. */
export interface BestAt {
  k: string;
  v: string;
}

export interface SkillGroup {
  label: string;
  items: string[];
}

/**
 * The section headings every surface prints.
 *
 * Here rather than in each app because there is exactly one right answer per
 * language and two places that would otherwise hold their own copy: the CV
 * page's `<h2>`s and apps/ssh-cv's page titles. Renaming a section is now one
 * edit, and the generated cv.json carries these to Go like everything else.
 *
 * Only the headings both surfaces genuinely share live here. The website's
 * skills table says "languages spoken" and "outside work", which read as
 * terms in a definition list; the terminal calls the same two things
 * "Languages" and "Interests", which read as page headings. Those are
 * different words on purpose, so they stay where they are rendered.
 */
export interface CvLabels {
  experience: string;
  education: string;
  certifications: string;
  bestAt: string;
  skills: string;
}

export const CV_LABELS: Record<CvLang, CvLabels> = {
  en: {
    experience: "Experience",
    education: "Education",
    certifications: "Certifications",
    bestAt: "Best at",
    skills: "Skills",
  },
  pt: {
    experience: "Experiência",
    education: "Formação",
    certifications: "Certificações",
    bestAt: "Melhor em",
    skills: "Competências",
  },
};

export const EXPERIENCE: Record<CvLang, Experience[]> = {
  en: [
    {
      role: "Application Engineer",
      org: "digital solutions studio",
      // company: fill the name in here; the SSH CV falls back to `org`.
      period: "feb 2026 - now",
      place: "hybrid",
      stack: ["Angular", "TypeScript", "Ionic", "Capacitor"],
      bullets: [
        "Websites, online stores and applications with management-system integration, supporting businesses' digital transformation.",
      ],
      detail: [
        "Angular and TypeScript on the web side; Ionic and Capacitor so one codebase ships as an app on iOS and Android.",
        "Most of it is integration work: a site or an app is only useful to a client once it reads and writes the management system the business already runs on.",
      ],
    },
    {
      role: "Software Engineer",
      org: "public-sector AI project",
      // company: fill the name in here; the SSH CV falls back to `org`.
      period: "sep 2025 - jan 2026",
      place: "remote",
      stack: ["Python"],
      bullets: [
        "Built a chatbot avatar and voice system: speech-to-text, response integration and realistic lip-sync for natural interactions between citizens and staff.",
        "Applied NLP, generative AI and data science to streamline administrative processes and digital governance.",
      ],
      detail: [
        "Python throughout: the speech-to-text step, the response integration, and the timing that drives the lip-sync.",
        "Both sides of the conversation were real users - citizens and public-sector staff - so the bar was interaction that felt natural rather than a demo that looked good.",
      ],
    },
    {
      role: "Software Engineering Intern",
      org: "cloud management provider",
      // company: fill the name in here; the SSH CV falls back to `org`.
      period: "aug 2024 - jul 2025",
      place: "hybrid",
      stack: ["Lit", "JavaScript", "Bun"],
      bullets: [
        "Built interactive onboarding sliders that cut onboarding time ~15% for a banking platform.",
        "Led a web component for managing document elements; shipped a new expression editor from usability testing.",
      ],
      detail: [
        "Lit and plain JavaScript, so the components stayed framework-agnostic and dropped into anything.",
        "Bun as the toolchain, early enough that it was still new.",
        "The onboarding sliders went into a banking platform's own flow; the expression editor came out of usability testing rather than a spec.",
      ],
    },
  ],
  pt: [
    {
      role: "Engenheiro de Aplicações",
      org: "estúdio de soluções digitais",
      period: "fev 2026 - agora",
      place: "híbrido",
      stack: ["Angular", "TypeScript", "Ionic", "Capacitor"],
      bullets: [
        "Sites, lojas online e aplicações com integração de sistemas de gestão, apoiando a transformação digital das empresas.",
      ],
      detail: [
        "Angular e TypeScript no lado web; Ionic e Capacitor para que uma base de código chegue a iOS e Android como aplicação.",
        "A maior parte é trabalho de integração: um site ou uma aplicação só é útil ao cliente quando lê e escreve no sistema de gestão que a empresa já usa.",
      ],
    },
    {
      role: "Engenheiro de Software",
      org: "projeto de IA no setor público",
      period: "set 2025 - jan 2026",
      place: "remoto",
      stack: ["Python"],
      bullets: [
        "Construí um avatar de chatbot e sistema de voz: speech-to-text, integração de respostas e lip-sync realista para interações naturais entre cidadãos e funcionários.",
        "Apliquei PLN, IA generativa e ciência de dados para simplificar processos administrativos e a governação digital.",
      ],
      detail: [
        "Python em todo o projeto: o speech-to-text, a integração das respostas e o tempo que comanda a sincronização labial.",
        "Dos dois lados da conversa estavam utilizadores reais - cidadãos e funcionários públicos - por isso a fasquia era interação que parecesse natural e não uma demonstração bonita.",
      ],
    },
    {
      role: "Estagiário de Engenharia de Software",
      org: "fornecedor de gestão cloud",
      period: "ago 2024 - jul 2025",
      place: "híbrido",
      stack: ["Lit", "JavaScript", "Bun"],
      bullets: [
        "Construí sliders de onboarding interativos que reduziram o tempo de onboarding em ~15% numa plataforma bancária.",
        "Liderei um componente web para gerir elementos de documentos; lancei um novo editor de expressões a partir de testes de usabilidade.",
      ],
      detail: [
        "Lit e JavaScript simples, para que os componentes ficassem agnósticos ao framework e encaixassem em qualquer sítio.",
        "Bun como toolchain, numa altura em que ainda era novo.",
        "Os sliders de onboarding entraram no fluxo de uma plataforma bancária; o editor de expressões saiu de testes de usabilidade e não de uma especificação.",
      ],
    },
  ],
};

export const EDUCATION: Record<CvLang, Education[]> = {
  en: [
    {
      title: "MSc, Software Engineering",
      period: "2025 - 2027 (exp.)",
      bullets: ["Software architecture, testing and engineering practice."],
    },
    {
      title: "BSc, Computer Science",
      period: "2022 - 2025",
      bullets: [
        "Algorithms, data structures, systems, databases and web foundations.",
      ],
    },
  ],
  pt: [
    {
      title: "Mestrado em Engenharia de Software",
      period: "2025 - 2027 (prev.)",
      bullets: ["Arquitetura de software, testes e prática de engenharia."],
    },
    {
      title: "Licenciatura em Ciência de Computadores",
      period: "2022 - 2025",
      bullets: [
        "Algoritmos, estruturas de dados, sistemas, bases de dados e fundamentos web.",
      ],
    },
  ],
};

export const CERTIFICATIONS: Record<CvLang, Certification[]> = {
  en: [
    {
      title: "Scrum certification",
      note: "Sprints, backlog refinement, reviews and retrospectives - delivery inside a team rather than around one.",
    },
  ],
  pt: [
    {
      title: "Certificação Scrum",
      note: "Sprints, refinamento de backlog, revisões e retrospetivas - entrega dentro de uma equipa e não ao lado dela.",
    },
  ],
};

export const BEST_AT: Record<CvLang, BestAt[]> = {
  en: [
    {
      k: "Full-stack product engineering",
      v: "Astro and Angular front-ends through to typed APIs and the databases behind them.",
    },
    {
      k: "Cross-platform apps",
      v: "Ionic and Capacitor daily, Tauri for desktop. One codebase, native where it matters.",
    },
    {
      k: "Web components & design systems",
      v: "Reusable, framework-agnostic UI primitives with real accessibility.",
    },
    {
      k: "Security & privacy",
      v: "CSP, edge middleware, dependency hygiene. Secure by default.",
    },
    {
      k: "Applied AI / NLP",
      v: "Chat, voice and generative features wired into real products.",
    },
  ],
  pt: [
    {
      k: "Engenharia de produto full-stack",
      v: "Front-ends em Astro e Angular até APIs tipadas e as bases de dados por trás.",
    },
    {
      k: "Aplicações multiplataforma",
      v: "Ionic e Capacitor no dia a dia, Tauri para desktop. Uma base de código, nativo onde é preciso.",
    },
    {
      k: "Web components e design systems",
      v: "Primitivas de UI reutilizáveis e agnósticas ao framework, com acessibilidade a sério.",
    },
    {
      k: "Segurança e privacidade",
      v: "CSP, middleware na edge, higiene de dependências. Seguro por defeito.",
    },
    {
      k: "IA aplicada / PLN",
      v: "Chat, voz e funcionalidades generativas integradas em produtos reais.",
    },
  ],
};

export const SKILLS: Record<CvLang, SkillGroup[]> = {
  en: [
    {
      label: "languages",
      items: [
        "TypeScript",
        "JavaScript",
        "Go",
        "Kotlin",
        "Python",
        "C#",
        "Rust",
        "Java",
      ],
    },
    {
      label: "frameworks",
      items: [
        "Astro",
        "Angular",
        "Ionic",
        "Capacitor",
        "Lit",
        "Next.js",
        "Svelte",
        "Tauri",
      ],
    },
    {
      label: "data",
      items: ["SQL Server", "SQLite", "PostgreSQL", "Firebase"],
    },
    {
      label: "infra",
      items: ["Docker", "Cloudflare Workers", "Bun"],
    },
  ],
  pt: [
    {
      label: "linguagens",
      items: [
        "TypeScript",
        "JavaScript",
        "Go",
        "Kotlin",
        "Python",
        "C#",
        "Rust",
        "Java",
      ],
    },
    {
      label: "frameworks",
      items: [
        "Astro",
        "Angular",
        "Ionic",
        "Capacitor",
        "Lit",
        "Next.js",
        "Svelte",
        "Tauri",
      ],
    },
    {
      label: "dados",
      items: ["SQL Server", "SQLite", "PostgreSQL", "Firebase"],
    },
    {
      label: "infra",
      items: ["Docker", "Cloudflare Workers", "Bun"],
    },
  ],
};

export const SPOKEN: Record<CvLang, string[]> = {
  en: ["Portuguese: native", "English: C1"],
  pt: ["Português: nativo", "Inglês: C1"],
};

export const INTERESTS: Record<CvLang, string[]> = {
  en: ["Weightlifting", "Nature walks", "Chess", "Formula 1", "Motorcycles"],
  pt: ["Musculação", "Caminhadas na natureza", "Xadrez", "Fórmula 1", "Motos"],
};
