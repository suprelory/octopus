import importlib.util
import re
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("updatePrice.py")
SPEC = importlib.util.spec_from_file_location("updatePrice", SCRIPT_PATH)
if SPEC is None or SPEC.loader is None:
    raise ImportError(f"Unable to load {SCRIPT_PATH}")
update_price = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(update_price)


class UpdatePriceTest(unittest.TestCase):
    @staticmethod
    def model(model_id: str, cost: dict) -> dict:
        return {"id": model_id, "cost": cost}

    @classmethod
    def provider(cls, *models: dict) -> dict:
        return {
            "models": {
                str(index): model
                for index, model in enumerate(models)
            }
        }

    def test_duplicate_model_id_uses_later_provider_and_warns(self) -> None:
        first_cost = {"input": 1, "output": 2, "cache_read": 0.1}
        second_cost = {"input": 3, "output": 4, "cache_read": 0.2}
        raw_price = {
            "alibaba": self.provider(
                self.model("alibaba-only", {"input": 0.5}),
                self.model("glm-5.2", first_cost),
            ),
            "zhipuai": self.provider(
                self.model("zhipuai-only", {"input": 0.75}),
                self.model("glm-5.2", second_cost),
            ),
        }

        entries, provider_counts, warnings = update_price.collect_price_entries(
            raw_price,
            providers=["alibaba", "zhipuai"],
        )

        self.assertEqual(
            list(entries),
            ["alibaba-only", "zhipuai-only", "glm-5.2"],
        )
        self.assertEqual(entries["glm-5.2"]["cost"], second_cost)
        self.assertEqual(entries["glm-5.2"]["provider"], "zhipuai")
        self.assertEqual(provider_counts, {"alibaba": 1, "zhipuai": 2})
        self.assertEqual(len(warnings), 1)
        self.assertIn('Duplicate model ID "glm-5.2"', warnings[0])
        self.assertIn("alibaba/glm-5.2", warnings[0])
        self.assertIn("zhipuai/glm-5.2", warnings[0])

    def test_model_ids_are_case_insensitive(self) -> None:
        raw_price = {
            "first": self.provider(
                self.model("GLM-5.2", {"input": 1}),
            ),
            "second": self.provider(
                self.model("glm-5.2", {"input": 2}),
            ),
        }

        entries, _, warnings = update_price.collect_price_entries(
            raw_price,
            providers=["first", "second"],
        )

        self.assertEqual(list(entries), ["glm-5.2"])
        self.assertEqual(entries["glm-5.2"]["cost"]["input"], 2)
        self.assertEqual(len(warnings), 1)

    def test_claude_aliases_are_stable_and_unique(self) -> None:
        raw_price = {
            "anthropic": self.provider(
                self.model("claude-opus-4-5", {"input": 1}),
            ),
        }

        entries, _, warnings = update_price.collect_price_entries(
            raw_price,
            providers=["anthropic"],
        )

        self.assertEqual(
            list(entries),
            [
                "claude-opus-4-5",
                "claude-opus-4.5",
                "claude-4.5-opus",
                "claude-4-5-opus",
            ],
        )
        self.assertEqual(len(entries), len(set(entries)))
        self.assertEqual(warnings, [])

    def test_alias_collision_uses_later_entry(self) -> None:
        raw_price = {
            "first": self.provider(
                self.model("base-model", {"input": 1}),
            ),
            "second": self.provider(
                self.model("alias-model", {"input": 2}),
            ),
        }

        entries, provider_counts, warnings = update_price.collect_price_entries(
            raw_price,
            providers=["first", "second"],
            model_aliases={"base-model": ["alias-model"]},
        )

        self.assertEqual(entries["alias-model"]["provider"], "second")
        self.assertEqual(entries["alias-model"]["cost"]["input"], 2)
        self.assertEqual(provider_counts, {"first": 1, "second": 1})
        self.assertEqual(len(warnings), 1)
        self.assertIn("first/base-model", warnings[0])
        self.assertIn("second/alias-model", warnings[0])

    def test_empty_ids_are_ignored_and_missing_cost_defaults_to_zero(self) -> None:
        raw_price = {
            "provider": {
                "models": {
                    "empty": {"id": ""},
                    "none": {"id": None},
                    "valid": {"id": "valid-model"},
                }
            }
        }

        entries, provider_counts, warnings = update_price.collect_price_entries(
            raw_price,
            providers=["provider"],
        )
        rendered = update_price.render_presets(entries, "test timestamp")

        self.assertEqual(list(entries), ["valid-model"])
        self.assertEqual(provider_counts, {"provider": 1})
        self.assertEqual(warnings, [])
        self.assertIn(
            '"valid-model": {Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0}',
            rendered,
        )

    def test_rendered_go_contains_unique_keys(self) -> None:
        raw_price = {
            "first": self.provider(
                self.model("same-model", {"input": 1}),
            ),
            "second": self.provider(
                self.model("same-model", {"input": 2}),
            ),
        }
        entries, _, _ = update_price.collect_price_entries(
            raw_price,
            providers=["first", "second"],
        )

        rendered = update_price.render_presets(entries, "test timestamp")
        keys = re.findall(r'^\s*"([^"]+)":', rendered, re.MULTILINE)

        self.assertEqual(keys, ["same-model"])
        self.assertEqual(len(keys), len(set(keys)))
        self.assertIn("Last updated: test timestamp", rendered)


if __name__ == "__main__":
    unittest.main()
