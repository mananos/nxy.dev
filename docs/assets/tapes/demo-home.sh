# Sourced by each CLI tape inside a Hide block, in the VHS container (repo mounted at /vhs).
# The vhs image has no git: render.sh uses nxy-docs-vhs (docs/assets/tapes/Dockerfile).
rm -rf /tmp/nxy-demo
mkdir -p /tmp/nxy-demo/home /tmp/nxy-demo/facturacion-api/src /tmp/nxy-demo/facturacion-api/tests

export NXY_HOME=/tmp/nxy-demo/home
export NXY_ICONS=unicode
export PATH=/vhs/docs/assets/.build:$PATH

cd /tmp/nxy-demo/facturacion-api || return 1
export CLAUDE_PROJECT_DIR=/tmp/nxy-demo/facturacion-api

cat > README.md <<'EOF'
# facturacion-api
API de facturacion: emite facturas, calcula impuestos y envia el comprobante por mail.
EOF
cat > src/factura.go <<'EOF'
package facturacion

// EmitirFactura arma la factura de un pedido y le calcula el IVA.
func EmitirFactura(pedido Pedido) Factura {
	return Factura{Pedido: pedido, IVA: CalcularIVA(pedido.Total)}
}
EOF
cat > src/impuestos.go <<'EOF'
package facturacion

// CalcularIVA devuelve el IVA de un importe (21%).
func CalcularIVA(total float64) float64 { return total * 0.21 }
EOF
cat > tests/factura_test.go <<'EOF'
package facturacion

import "testing"

func TestCalcularIVA(t *testing.T) {
	if CalcularIVA(100) != 21 {
		t.Fatal("IVA mal calculado")
	}
}
EOF

git init -q . >/dev/null 2>&1
git config user.email demo@example.com
git config user.name demo
git add -A
git commit -q -m "facturacion-api: version inicial" >/dev/null 2>&1

nxy mem rule "Los importes se guardan en centavos, nunca en float" --question "Como guardamos los importes?" >/dev/null
nxy mem rule "Todo endpoint nuevo lleva un test en tests/" --question "Que exigimos a un endpoint nuevo?" >/dev/null
nxy mem rule "El IVA se calcula en impuestos.go, no en los handlers" --question "Donde se calcula el IVA?" --paths "src/impuestos.go" >/dev/null
clear
