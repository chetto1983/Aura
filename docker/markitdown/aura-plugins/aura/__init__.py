"""
Aura markitdown plugin package.

Entry point: markitdown.plugin = aura:register

markitdown's enable_plugins protocol calls register.register_converters(instance).
The register object is also callable as register(instance) per the story spec.
"""

from aura.docx_converter import AuraDocxConverter
from aura.xlsx_converter import AuraXlsxConverter


class _AuraPlugin:
    """
    Satisfies both markitdown's plugin protocol (.register_converters)
    and the story spec (callable as register(markitdown_instance)).
    """

    def __call__(self, markitdown_instance) -> None:
        self.register_converters(markitdown_instance)

    def register_converters(self, markitdown_instance, **kwargs) -> None:
        markitdown_instance.register_converter(AuraDocxConverter())
        markitdown_instance.register_converter(AuraXlsxConverter())


register = _AuraPlugin()

__all__ = ["register", "AuraDocxConverter", "AuraXlsxConverter"]
