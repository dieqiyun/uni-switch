/* WARNING: Script requires that SQLITE_DBCONFIG_DEFENSIVE be disabled */
PRAGMA foreign_keys=OFF;
BEGIN TRANSACTION;
CREATE TABLE `session_message` (
          `id` text PRIMARY KEY,
          `session_id` text NOT NULL,
          `type` text NOT NULL,
          `seq` integer NOT NULL,
          `time_created` integer NOT NULL,
          `time_updated` integer NOT NULL,
          `data` text NOT NULL,
          CONSTRAINT `fk_session_message_session_id_session_v2_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session_v2`(`id`) ON DELETE CASCADE
        );
INSERT INTO session_message VALUES('msg_18ac5bbca001QzFmQpSaOIdxHp','ses_e753a44afffeaDlZRyBkvVR0wE','user',5,1793329576940,1793329576940,'{"time":{"created":1793329576940},"text":"\"First run `cat notes.txt`, then greet me using the greet-user skill.\"","files":[]}');
INSERT INTO session_message VALUES('msg_18ac5bbf0001OAUTxjatLELX3H','ses_e753a44afffeaDlZRyBkvVR0wE','assistant',6,1793329576949,1793329578505,'{"time":{"created":1793329576949,"streamed":1793329578498,"completed":1793329578504},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"I''ll start by reading the notes file, then greet you using the skill."},{"type":"tool","id":"tooluse_15dzmf8VW4TtFaJb2rYD21","name":"read","executed":false,"state":{"status":"completed","input":{"path":"notes.txt"},"content":[{"type":"text","text":"Read file notes.txt, lines 1-1\n1: hello"}],"metadata":{"truncated":false}},"time":{"created":1793329578442,"ran":1793329578495,"completed":1793329578503}}],"finish":"tool-calls","rawFinish":"tool_calls","cost":0,"tokens":{"input":10050,"output":23,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac5c20f001AHgEvrX6CbKisH','ses_e753a44afffeaDlZRyBkvVR0wE','assistant',15,1793329578516,1793329579702,'{"time":{"created":1793329578516,"streamed":1793329579698,"completed":1793329579701},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"Now let me greet you using the greet-user skill:"},{"type":"tool","id":"tooluse_hgNGCVQeancVpA2T09Qa8O","name":"skill","executed":false,"state":{"status":"completed","input":{"id":"greet-user"},"content":[{"type":"text","text":"<skill_content name=\"greet-user\">\n# Skill: greet-user\n\nReply with exactly: \"Hello from greet-user!\"\n\nBase directory for this skill: /private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/home/.config/opencode/skills/greet-user\nRelative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.\nNote: file list is sampled.\n\n<skill_files>\n</skill_files>\n</skill_content>"}],"metadata":{"name":"greet-user","directory":"/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/home/.config/opencode/skills/greet-user","truncated":false}},"time":{"created":1793329579606,"ran":1793329579696,"completed":1793329579700}}],"finish":"tool-calls","rawFinish":"tool_calls","cost":0,"tokens":{"input":10145,"output":17,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac5c6ba00146PiDnBKiHtU4l','ses_e753a44afffeaDlZRyBkvVR0wE','assistant',24,1793329579711,1793329581067,'{"time":{"created":1793329579711,"streamed":1793329581066,"completed":1793329581067},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"Hello from greet-user!\n\nI found `hello` in your notes.txt file, and I''ve greeted you as requested! 👋"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":10352,"output":26,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac5cc0c002vkwBsSFTHsVBh2','ses_e753a44afffeaDlZRyBkvVR0wE','idle',29,1793329581068,1793329581068,'{"time":{"created":1793329581068},"outcome":"succeeded"}');
INSERT INTO session_message VALUES('msg_18ac5f14d001bZuYT9bEMZ7Day','ses_e753a0f28ffe4fFER8Ll41K3GY','user',5,1793329590642,1793329590642,'{"time":{"created":1793329590642},"text":"@greet-user","files":[]}');
INSERT INTO session_message VALUES('msg_18ac5f175001me2Ey6oYOa76WO','ses_e753a0f28ffe4fFER8Ll41K3GY','assistant',6,1793329590650,1793329592205,'{"time":{"created":1793329590650,"streamed":1793329592204,"completed":1793329592205},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"# Hey there! 👋\n\nWelcome! I''m Kiro, your AI coding agent. I''m here to help you write code, debug issues, explore codebases, plan solutions, and tackle whatever professional work you need. I work alongside you—we can exchange ideas, identify problems, and narrow down the right approach before diving into implementation.\n\nWhat are we working on today?"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":10046,"output":88,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac5f78e002N79llp1Eq1RGUx','ses_e753a0f28ffe4fFER8Ll41K3GY','idle',11,1793329592206,1793329592206,'{"time":{"created":1793329592206},"outcome":"succeeded"}');
INSERT INTO session_message VALUES('msg_18ac62dc9001vuhLAzUcVoUnPQ','ses_e7539d2a5ffePUVJFMb1gKVccL','user',5,1793329606120,1793329606121,'{"time":{"created":1793329606120},"text":"/greet-user","files":[]}');
INSERT INTO session_message VALUES('msg_18ac62dec001OQT8XKBTtbcZfc','ses_e7539d2a5ffePUVJFMb1gKVccL','assistant',6,1793329606128,1793329607431,'{"time":{"created":1793329606128,"streamed":1793329607427,"completed":1793329607430},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"I''ll greet you warmly using the greeting skill!"},{"type":"tool","id":"tooluse_w0sLqnzev8BE1xAtafNm0s","name":"skill","executed":false,"state":{"status":"completed","input":{"id":"greet-user"},"content":[{"type":"text","text":"<skill_content name=\"greet-user\">\n# Skill: greet-user\n\nReply with exactly: \"Hello from greet-user!\"\n\nBase directory for this skill: /private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/home/.config/opencode/skills/greet-user\nRelative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.\nNote: file list is sampled.\n\n<skill_files>\n</skill_files>\n</skill_content>"}],"metadata":{"name":"greet-user","directory":"/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/home/.config/opencode/skills/greet-user","truncated":false}},"time":{"created":1793329607406,"ran":1793329607425,"completed":1793329607429}}],"finish":"tool-calls","rawFinish":"tool_calls","cost":0,"tokens":{"input":10029,"output":17,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac6330a001YQSKzTEHbuokxL','ses_e7539d2a5ffePUVJFMb1gKVccL','assistant',15,1793329607438,1793329608701,'{"time":{"created":1793329607438,"streamed":1793329608700,"completed":1793329608701},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"Hello from greet-user! 👋\n\nWelcome! I''m here to help you with your coding tasks, projects, and any questions you might have. What would you like to work on today?"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":10245,"output":41,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac637ff0019Hk4nF2iBvXDET','ses_e7539d2a5ffePUVJFMb1gKVccL','idle',20,1793329608703,1793329608703,'{"time":{"created":1793329608703},"outcome":"succeeded"}');
INSERT INTO session_message VALUES('msg_18ac6d77b0012Meb3VpKvTSLQI','ses_e75393cbaffe9qtPD0p9PI6xYK','user',4,1793329649686,1793329649686,'{"time":{"created":1793329649686},"text":"@greet-user ","skills":[{"id":"greet-user","name":"greet-user","text":"<skill_content name=\"greet-user\">\n# Skill: greet-user\n\nReply with exactly: \"Hello from greet-user!\"\n\nBase directory for this skill: /private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/home/.config/opencode/skills/greet-user\nRelative paths in this skill (e.g., scripts/, reference/) are relative to this base directory.\nNote: file list is sampled.\n\n<skill_files>\n</skill_files>\n</skill_content>","mention":{"start":0,"end":11,"text":"@greet-user"}}]}');
INSERT INTO session_message VALUES('msg_18ac6d819001dbaJr3TgErAB10','ses_e75393cbaffe9qtPD0p9PI6xYK','assistant',5,1793329649694,1793329650812,'{"time":{"created":1793329649694,"streamed":1793329650810,"completed":1793329650811},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie","variant":"default"},"content":[{"type":"text","text":"Hello from greet-user!"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":10135,"output":6,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac6dc7d001f6ftnUHw8t1A3F','ses_e75393cbaffe9qtPD0p9PI6xYK','idle',10,1793329650813,1793329650813,'{"time":{"created":1793329650813},"outcome":"succeeded"}');
INSERT INTO session_message VALUES('msg_18ac75416001Z3bT97KDG7xutY','ses_e7538abfbffe2oKYxDK1OHgsrO','skill',1,1793329681430,1793329681431,'{"time":{"created":1793329681430},"skill":"greet-user","name":"greet-user","text":"Reply with exactly: \"Hello from greet-user!\"\n"}');
INSERT INTO session_message VALUES('msg_18ac7541d0010do6eOiB4QwSq6','ses_e7538abfbffe2oKYxDK1OHgsrO','assistant',4,1793329681441,1793329682536,'{"time":{"created":1793329681441,"streamed":1793329682535,"completed":1793329682536},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie","variant":"default"},"content":[{"type":"text","text":"Hello from greet-user!"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":9979,"output":6,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac7586a0011C9ZWAqkiMQl0y','ses_e7538abfbffe2oKYxDK1OHgsrO','idle',9,1793329682538,1793329682538,'{"time":{"created":1793329682538},"outcome":"succeeded"}');
INSERT INTO session_message VALUES('msg_18ac794f800118iPd2Jl9YiBSx','ses_e75386b77ffeTIwbQnkzH3CYw8','user',5,1793329698073,1793329698073,'{"time":{"created":1793329698073},"text":"/nonexistentzz","files":[]}');
INSERT INTO session_message VALUES('msg_18ac7951d001jZQhxyqGfoXkqb','ses_e75386b77ffeTIwbQnkzH3CYw8','assistant',6,1793329698082,1793329700227,'{"time":{"created":1793329698082,"streamed":1793329700226,"completed":1793329700226},"agent":"build","model":{"id":"auto/claude-haiku-4-5","providerID":"magpie"},"content":[{"type":"text","text":"I''m ready to help! I notice you''ve started with `/nonexistentzz`, which doesn''t appear to be a valid path or command.\n\nCould you clarify what you''d like me to help with? For example:\n\n- **Working with code**: Read, write, or edit files; run builds or tests\n- **Exploring a codebase**: Search for files, patterns, or specific code\n- **Git operations**: Create branches, commits, or pull requests\n- **General tasks**: Writing, analysis, planning, or research\n\nWhat would you like to accomplish?"}],"finish":"stop","rawFinish":"stop","cost":0,"tokens":{"input":10090,"output":123,"reasoning":0,"cache":{"read":0,"write":0}}}');
INSERT INTO session_message VALUES('msg_18ac79d84001RfKvjiKT9cZvn8','ses_e75386b77ffeTIwbQnkzH3CYw8','idle',11,1793329700228,1793329700228,'{"time":{"created":1793329700228},"outcome":"succeeded"}');
CREATE TABLE `session_v2` (
          `id` text PRIMARY KEY,
          `project_id` text NOT NULL,
          `workspace_id` text,
          `parent_id` text,
          `fork_session_id` text,
          `fork_boundary` text,
          `slug` text NOT NULL,
          `directory` text NOT NULL,
          `path` text,
          `title` text,
          `version` text NOT NULL,
          `share_url` text,
          `summary_additions` integer,
          `summary_deletions` integer,
          `summary_files` integer,
          `summary_diffs` text,
          `metadata` text,
          `cost` real DEFAULT 0 NOT NULL,
          `tokens_input` integer DEFAULT 0 NOT NULL,
          `tokens_output` integer DEFAULT 0 NOT NULL,
          `tokens_reasoning` integer DEFAULT 0 NOT NULL,
          `tokens_cache_read` integer DEFAULT 0 NOT NULL,
          `tokens_cache_write` integer DEFAULT 0 NOT NULL,
          `revert` text,
          `permission` text,
          `agent` text,
          `model` text,
          `time_created` integer NOT NULL,
          `time_updated` integer NOT NULL,
          `time_idle` integer,
          `time_viewed` integer,
          `idle_outcome` text,
          `time_compacting` integer,
          `time_archived` integer,
          `time_suspended` integer,
          `resume_attempts` integer DEFAULT 0 NOT NULL,
          CONSTRAINT `fk_session_v2_project_id_project_id_fk` FOREIGN KEY (`project_id`) REFERENCES `project`(`id`) ON DELETE CASCADE
        );
INSERT INTO session_v2 VALUES('ses_e753a44afffeaDlZRyBkvVR0wE','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'curious-lagoon','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s1-skill-tool','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,30547,66,0,0,0,NULL,NULL,NULL,NULL,1793329576898,1793329581067,1793329581068,NULL,'succeeded',NULL,NULL,NULL,0);
INSERT INTO session_v2 VALUES('ses_e753a0f28ffe4fFER8Ll41K3GY','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'shiny-cactus','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s2-at-skill','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,10046,88,0,0,0,NULL,NULL,NULL,NULL,1793329590597,1793329592205,1793329592206,NULL,'succeeded',NULL,NULL,NULL,0);
INSERT INTO session_v2 VALUES('ses_e7539d2a5ffePUVJFMb1gKVccL','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'clever-canyon','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s2b-slash-skill','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,20274,58,0,0,0,NULL,NULL,NULL,NULL,1793329606078,1793329608701,1793329608703,NULL,'succeeded',NULL,NULL,NULL,0);
INSERT INTO session_v2 VALUES('ses_e75393cbaffe9qtPD0p9PI6xYK','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'hidden-star','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s2-user-at-skill','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,10135,6,0,0,0,NULL,NULL,NULL,'{"id":"auto/claude-haiku-4-5","providerID":"magpie"}',1793329644360,1793329650811,1793329650813,NULL,'succeeded',NULL,NULL,NULL,0);
INSERT INTO session_v2 VALUES('ses_e7538abfbffe2oKYxDK1OHgsrO','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'gentle-rocket','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s2d-activate-skill-api','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,9979,6,0,0,0,NULL,NULL,NULL,'{"id":"auto/claude-haiku-4-5","providerID":"magpie"}',1793329681413,1793329682536,1793329682538,NULL,'succeeded',NULL,NULL,NULL,0);
INSERT INTO session_v2 VALUES('ses_e75386b77ffeTIwbQnkzH3CYw8','36633faa34572c39e3b8f90b38784051f1b64a9a',NULL,NULL,NULL,NULL,'curious-rocket','/private/tmp/claude-501/-Users-yetone-workspace-projects-magpie/4c056ab3-f2e6-4918-b774-da5c713ce1e3/scratchpad/cap/opencode/work','','s3-nonexistent','2.0.25',NULL,NULL,NULL,NULL,NULL,NULL,0.0,10090,123,0,0,0,NULL,NULL,NULL,NULL,1793329698035,1793329700226,1793329700228,NULL,'succeeded',NULL,NULL,NULL,0);
COMMIT;
