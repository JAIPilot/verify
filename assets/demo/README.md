# Spring Petclinic demo storyboard

The two GIFs in the main README are **illustrated product walkthroughs**. They are scripted visuals of the current Verify flow, not a captured Codex or Verify session. They do not assert actual test counts, coverage numbers, mutation scores, or a measured Petclinic result.

## Scenario

Codex adds owner lookup by phone to Spring Petclinic and edits the existing owner search path. The candidate still finds an owner by phone, but partial surname search changes: `Dav` no longer finds `Davis`.

Verify's baseline tester characterizes that old search behavior and replays the frozen test on the candidate. The reviewer asks whether partial matching should remain. The user says that phone lookup is additive and approves a proposed repair. The original tester then locks both the old and new accepted behavior, reruns local checks, and the reporter prepares the verdict. The final diff is shown before the user chooses whether to apply it.

This order follows [Verify's implementation and stage gates](../../docs/how-it-works.md). The exact file edits and terminal text are illustrative. In a real run, the questions, edits, evidence, and verdict depend on the repository and the user's answers.

## Regenerate

The editable storyboard is [render.py](render.py). It needs Python 3, Pillow, and ffmpeg:

```sh
python3 -m pip install Pillow
python3 assets/demo/render.py
```

The script writes `assets/verify-petclinic-story.gif` and `assets/verify-review-loop.gif`.
