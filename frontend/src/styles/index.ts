import "./theme.css";
import { layout } from "./layout.css";
import { common } from "./common.css";
import { archive } from "./archive.css";
import { forms } from "./forms.css";
import { forum } from "./forum.css";

import { overlays } from "./overlays.css";

export const s = { ...layout, ...common, ...archive, ...forms, ...forum, ...overlays };
