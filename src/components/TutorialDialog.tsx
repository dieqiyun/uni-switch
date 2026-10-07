import { useRef, useState } from "react";
import { ArrowRight, ExternalLink } from "lucide-react";
import { Modal } from "./Modal";
import { api, desktopRuntime, errorMessage } from "../lib/api";
import tutorial from "../content/tutorial.json";

interface TutorialSection {
  title: string;
  paragraphs?: string[];
  steps?: string[];
  bullets?: string[];
  table?: { headers: string[]; rows: string[][] };
}
interface TutorialTopic {
  id: string;
  title: string;
  summary: string;
  sections: TutorialSection[];
}
const topics: TutorialTopic[] = tutorial.topics;
const tutorialUrl =
  "https://github.com/dieqiyun/uni-switch/blob/main/docs/tutorial.md";

export function TutorialDialog({
  onClose,
  initialTopic,
}: {
  onClose: () => void;
  initialTopic?: string;
}) {
  const [selected, setSelected] = useState(
    topics.find((t) => t.id === initialTopic)?.id || topics[0].id,
  );
  const [error, setError] = useState("");
  const [opening, setOpening] = useState(false);
  const article = useRef<HTMLElement>(null);
  const topic = topics.find((t) => t.id === selected)!;
  function select(id: string) {
    setSelected(id);
    article.current?.scrollTo?.({ top: 0 });
  }
  return (
    <Modal
      title="使用说明"
      description={tutorial.intro}
      onClose={onClose}
      wide
      className="tutorial-modal"
    >
      <div className="tutorial-layout">
        <nav className="tutorial-nav" aria-label="教程目录">
          {topics.map((t) => (
            <button
              key={t.id}
              type="button"
              aria-current={t.id === selected ? "page" : undefined}
              onClick={() => select(t.id)}
            >
              {t.title}
            </button>
          ))}
        </nav>
        <article
          className="tutorial-article"
          aria-labelledby="tutorial-topic-title"
          ref={article}
          tabIndex={0}
        >
          <h3 id="tutorial-topic-title">{topic.title}</h3>
          <p className="tutorial-summary">{topic.summary}</p>
          {topic.sections.map((section) => (
            <section key={section.title}>
              <h4>{section.title}</h4>
              {section.steps && (
                <ol>
                  {section.steps.map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
              )}
              {section.paragraphs?.map((p) => (
                <p key={p}>{p}</p>
              ))}
              {section.bullets && (
                <ul>
                  {section.bullets.map((item) => (
                    <li key={item}>{item}</li>
                  ))}
                </ul>
              )}
              {section.table && (
                <table>
                  <caption className="sr-only">{section.title}</caption>
                  <thead>
                    <tr>
                      {section.table.headers.map((h) => (
                        <th key={h} scope="col">
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {section.table.rows.map((row) => (
                      <tr key={row[0]}>
                        {row.map((cell, i) =>
                          i === 0 ? (
                            <th key={i} scope="row">
                              {cell}
                            </th>
                          ) : (
                            <td key={i}>{cell}</td>
                          ),
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </section>
          ))}
        </article>
      </div>
      {error && (
        <p className="error-notice" role="alert">
          {error}，可复制教程地址：{tutorialUrl}
        </p>
      )}
      <div className="tutorial-footer">
        <a
          href={tutorialUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="button secondary"
          aria-disabled={opening || undefined}
          onClick={(event) => {
            if (!desktopRuntime) return;
            event.preventDefault();
            if (opening) return;
            setOpening(true);
            setError("");
            void api
              .openProjectPage("tutorial")
              .catch((e) => setError(errorMessage(e)))
              .finally(() => setOpening(false));
          }}
        >
          <ExternalLink size={15} aria-hidden />
          {opening ? "打开中…" : "GitHub 完整教程"}
        </a>
        <button className="button primary" onClick={onClose}>
          开始使用
          <ArrowRight size={16} aria-hidden />
        </button>
      </div>
    </Modal>
  );
}
