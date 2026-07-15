<?php

declare(strict_types=1);

namespace RetrievalHoldout\CallableValue;

require_once __DIR__ . '/LabelFormatter.php';

final class DeferredLabel
{
    public function callback(): \Closure
    {
        return LabelFormatter::formatLabel(...);
    }
}
