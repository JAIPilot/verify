# Spring Petclinic live demo

The single demo GIF uses selected frames from real local terminal recordings made on October 20, 2026, using the earlier hosted version of Verify. It contains captured coding agent and Verify output and actual user decisions. Explanatory captions were added, and idle time and long output were skipped. No captured terminal text, test result, or decision was invented. The open source local-agent CLI has a different terminal flow.

## Starting point

- Project: [Spring Framework Petclinic](https://github.com/spring-petclinic/spring-framework-petclinic), commit `2d75ede`.
- Demo baseline: a local commit adding `AGENTS.md` with the project's existing JaCoCo command, `./mvnw -q package`. No production files or build configuration changed in that commit.
- Codex change: a new `PetTypeAliasResolver`, an update to `PetTypeFormatter.parse`, and focused formatter tests. Codex ran `./mvnw -q -Dtest=PetTypeFormatterTests test` and `./mvnw -q package`; both passed.
- Verify command: `verify --base main`, using the managed JAIPilot backend and local Maven/JaCoCo. The review questions, edit approval, full-diff acceptance, and final promotion were answered in the recorded terminal.

## Observed evidence

| Stage | Result |
| --- | --- |
| Baseline formatter | 10/10 lines and 4/4 branches covered |
| Frozen baseline replay on candidate | Existing suite and characterization tests passed |
| Final formatter | 14/14 lines and 8/8 branches covered |
| Final resolver | 2/2 lines and 8/8 branches covered |
| Final suite and verdict | `./mvnw -q package` passed; `good_to_go` |

Verify added `PetTypeFormatterCharacterizationTests.java` on the baseline. The reviewer proposed two edits to `PetTypeFormatterTests.java`: first for an alias whose target type is absent, then to assert the original input and exception offset after user clarification. The final tester added `PetTypeAliasResolverTests.java` and ran the full suite and coverage gate. The verified changes were applied to the local demo checkout. These measured results describe this run; they are not a guarantee for other changes or repositories.

The original `.cast` recordings and `report.json` remain in the local demo workspace. Terminal frames were rendered from those recordings with [asciinema](https://docs.asciinema.org/manual/cli/) and [agg](https://github.com/asciinema/agg), then assembled into the one captioned GIF. It shows the code change, regression check, user decision, final tests, and report in order.
