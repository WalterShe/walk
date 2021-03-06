// Copyright 2010 The Walk Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package walk

import (
	"fmt"
	"math/big"
	"syscall"
	"time"
	"unsafe"
)

import . "github.com/lxn/go-winapi"

type ComboBox struct {
	WidgetBase
	bindingValueProvider         BindingValueProvider
	model                        ListModel
	format                       string
	precision                    int
	itemsResetHandlerHandle      int
	itemChangedHandlerHandle     int
	maxItemTextWidth             int
	prevCurIndex                 int
	selChangeIndex               int
	currentIndexChangedPublisher EventPublisher
	currentIndexProperty         *Property
	valueProperty                *Property
}

func NewComboBox(parent Container) (*ComboBox, error) {
	cb := &ComboBox{prevCurIndex: -1, selChangeIndex: -1, precision: 2}

	if err := InitChildWidget(
		cb,
		parent,
		"COMBOBOX",
		WS_TABSTOP|WS_VISIBLE|WS_VSCROLL|CBS_DROPDOWNLIST,
		0); err != nil {
		return nil, err
	}

	cb.valueProperty = NewProperty(
		"Value",
		func() interface{} {
			index := cb.CurrentIndex()

			if cb.bindingValueProvider == nil || index == -1 {
				return nil
			}

			return cb.bindingValueProvider.BindingValue(index)
		},
		func(v interface{}) error {
			if cb.bindingValueProvider == nil {
				return newError("Data binding is only supported using a model that implements BindingValueProvider.")
			}

			index := -1

			count := cb.model.ItemCount()
			for i := 0; i < count; i++ {
				if cb.bindingValueProvider.BindingValue(i) == v {
					index = i
					break
				}
			}

			return cb.SetCurrentIndex(index)
		},
		cb.CurrentIndexChanged())

	cb.currentIndexProperty = NewProperty(
		"CurrentIndex",
		func() interface{} {
			return cb.CurrentIndex()
		},
		func(v interface{}) error {
			return cb.SetCurrentIndex(v.(int))
		},
		cb.CurrentIndexChanged())

	cb.MustRegisterProperties(cb.valueProperty, cb.currentIndexProperty)

	return cb, nil
}

func (*ComboBox) LayoutFlags() LayoutFlags {
	return GrowableHorz
}

func (cb *ComboBox) MinSizeHint() Size {
	defaultSize := cb.dialogBaseUnitsToPixels(Size{50, 12})

	if cb.model != nil && cb.maxItemTextWidth <= 0 {
		cb.maxItemTextWidth = cb.calculateMaxItemTextWidth()
	}

	// FIXME: Use GetThemePartSize instead of guessing
	w := maxi(defaultSize.Width, cb.maxItemTextWidth+24)
	h := defaultSize.Height + 1

	return Size{w, h}
}

func (cb *ComboBox) SizeHint() Size {
	return cb.MinSizeHint()
}

func (cb *ComboBox) itemString(index int) string {
	switch val := cb.model.Value(index).(type) {
	case string:
		return val

	case time.Time:
		return val.Format(cb.format)

	case *big.Rat:
		return val.FloatString(cb.precision)

	default:
		return fmt.Sprintf(cb.format, val)
	}

	panic("unreachable")
}

func (cb *ComboBox) insertItemAt(index int) error {
	str := cb.itemString(index)
	lp := uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(str)))

	if CB_ERR == cb.SendMessage(CB_INSERTSTRING, uintptr(index), lp) {
		return newError("SendMessage(CB_INSERTSTRING)")
	}

	return nil
}

func (cb *ComboBox) resetItems() error {
	cb.SetSuspended(true)
	defer cb.SetSuspended(false)

	if FALSE == cb.SendMessage(CB_RESETCONTENT, 0, 0) {
		return newError("SendMessage(CB_RESETCONTENT)")
	}

	cb.maxItemTextWidth = 0

	cb.SetCurrentIndex(-1)

	if cb.model == nil {
		return nil
	}

	count := cb.model.ItemCount()

	for i := 0; i < count; i++ {
		if err := cb.insertItemAt(i); err != nil {
			return err
		}
	}

	return nil
}

func (cb *ComboBox) attachModel() {
	itemsResetHandler := func() {
		cb.resetItems()
	}
	cb.itemsResetHandlerHandle = cb.model.ItemsReset().Attach(itemsResetHandler)

	itemChangedHandler := func(index int) {
		if CB_ERR == cb.SendMessage(CB_DELETESTRING, uintptr(index), 0) {
			newError("SendMessage(CB_DELETESTRING)")
		}

		cb.insertItemAt(index)

		cb.SetCurrentIndex(cb.prevCurIndex)
	}
	cb.itemChangedHandlerHandle = cb.model.ItemChanged().Attach(itemChangedHandler)
}

func (cb *ComboBox) detachModel() {
	cb.model.ItemsReset().Detach(cb.itemsResetHandlerHandle)
	cb.model.ItemChanged().Detach(cb.itemChangedHandlerHandle)
}

func (cb *ComboBox) Model() ListModel {
	return cb.model
}

func (cb *ComboBox) SetModel(model ListModel) error {
	if cb.model != nil {
		cb.detachModel()
	}

	cb.model = model
	cb.bindingValueProvider, _ = model.(BindingValueProvider)

	if model != nil {
		cb.attachModel()
	}

	return cb.resetItems()
}

func (cb *ComboBox) Format() string {
	return cb.format
}

func (cb *ComboBox) SetFormat(value string) {
	cb.format = value
}

func (cb *ComboBox) Precision() int {
	return cb.precision
}

func (cb *ComboBox) SetPrecision(value int) {
	cb.precision = value
}

func (cb *ComboBox) calculateMaxItemTextWidth() int {
	hdc := GetDC(cb.hWnd)
	if hdc == 0 {
		newError("GetDC failed")
		return -1
	}
	defer ReleaseDC(cb.hWnd, hdc)

	hFontOld := SelectObject(hdc, HGDIOBJ(cb.Font().handleForDPI(0)))
	defer SelectObject(hdc, hFontOld)

	var maxWidth int

	count := cb.model.ItemCount()
	for i := 0; i < count; i++ {
		var s SIZE
		str := syscall.StringToUTF16(cb.itemString(i))

		if !GetTextExtentPoint32(hdc, &str[0], int32(len(str)-1), &s) {
			newError("GetTextExtentPoint32 failed")
			return -1
		}

		maxWidth = maxi(maxWidth, int(s.CX))
	}

	return maxWidth
}

func (cb *ComboBox) CurrentIndex() int {
	return int(cb.SendMessage(CB_GETCURSEL, 0, 0))
}

func (cb *ComboBox) SetCurrentIndex(value int) error {
	index := int(cb.SendMessage(CB_SETCURSEL, uintptr(value), 0))

	if index != value {
		return newError("invalid index")
	}

	if value != cb.prevCurIndex {
		cb.prevCurIndex = value
		cb.currentIndexChangedPublisher.Publish()
	}

	return nil
}

func (cb *ComboBox) CurrentIndexChanged() *Event {
	return cb.currentIndexChangedPublisher.Event()
}

func (cb *ComboBox) Text() string {
	return widgetText(cb.hWnd)
}

func (cb *ComboBox) SetText(value string) error {
	return setWidgetText(cb.hWnd, value)
}

func (cb *ComboBox) TextSelection() (start, end int) {
	cb.SendMessage(CB_GETEDITSEL, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&end)))
	return
}

func (cb *ComboBox) SetTextSelection(start, end int) {
	cb.SendMessage(CB_SETEDITSEL, 0, uintptr(MAKELONG(uint16(start), uint16(end))))
}

func (cb *ComboBox) WndProc(hwnd HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		code := HIWORD(uint32(wParam))
		selIndex := cb.CurrentIndex()

		switch code {
		case CBN_SELCHANGE:
			cb.selChangeIndex = selIndex

		case CBN_SELENDCANCEL:
			if cb.selChangeIndex != -1 {
				cb.SetCurrentIndex(cb.selChangeIndex)

				cb.selChangeIndex = -1
			}

		case CBN_SELENDOK:
			if selIndex != cb.prevCurIndex {
				cb.currentIndexChangedPublisher.Publish()
				cb.prevCurIndex = selIndex
				return 0
			}

			cb.selChangeIndex = -1
		}
	}

	return cb.WidgetBase.WndProc(hwnd, msg, wParam, lParam)
}
